package policy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/config"
	"github.com/sig-cloudnative/nuts/pkg/log"
)

// DefaultPolicyEngine 默认策略引擎实现
type DefaultPolicyEngine struct {
	manager PolicyManager
	store   PolicyStore
	mu      sync.RWMutex
	logger  log.Logger

	// 并发控制
	maxConcurrentEvals int // 最大并发评估数（从配置读取，默认 50）
	ctx                context.Context
	cancel             context.CancelFunc

	// 统计信息
	stats EngineStats
}

// EngineStats 引擎统计信息
type EngineStats struct {
	TotalMatches     int64
	TotalEvaluations int64
	MatchRate        float64
	AverageLatency   int64 // 纳秒
}

// NewDefaultPolicyEngine 创建默认策略引擎
func NewDefaultPolicyEngine(manager PolicyManager, store PolicyStore) *DefaultPolicyEngine {
	logger := log.GetDefault()
	if logger == nil {
		// 创建一个简单的 logger 作为默认
		logger, _ = log.NewZapLogger("info")
	}
	return &DefaultPolicyEngine{
		manager:            manager,
		store:              store,
		logger:             logger,
		maxConcurrentEvals: 50,
	}
}

// SetLogger 设置日志记录器
func (e *DefaultPolicyEngine) SetLogger(logger log.Logger) {
	e.logger = logger
}

// Match 匹配事件与策略
func (e *DefaultPolicyEngine) Match(ctx context.Context, event *common.Event) ([]*PolicyMatch, error) {
	startTime := time.Now()

	// 获取所有启用的策略
	policies, err := e.store.ListEnabled()
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled policies for event %s: %w",
			event.ID, err)
	}

	e.logger.Debug("Evaluating policies",
		log.String("event_id", event.ID),
		log.String("event_type", event.Type),
		log.Int("policy_count", len(policies)))

	// 构建事件数据
	accessor := NewEventAccessor(event)
	eventData := accessor.ToMap()

	// 带并发上限的并行评估
	results := make([]*PolicyMatch, 0, len(policies))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var evalErrors []error

	sem := make(chan struct{}, e.maxConcurrentEvals)

	for _, policy := range policies {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return nil, ctx.Err()
		}

		wg.Add(1)
		go func(p *Policy) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					mu.Lock()
					evalErrors = append(evalErrors, fmt.Errorf("policy %s evaluation panic: %v", p.ID, r))
					mu.Unlock()
				}
			}()

			match := e.evaluatePolicy(ctx, p, eventData, startTime)
			mu.Lock()
			results = append(results, match)
			mu.Unlock()
		}(policy)
	}

	wg.Wait()

	// 处理评估错误
	if len(evalErrors) > 0 {
		e.logger.Warn("Policy evaluation errors occurred",
			log.Int("error_count", len(evalErrors)))
		// 返回部分结果，但记录错误
	}

	duration := time.Since(startTime)
	e.updateStats(results, duration)

	e.logger.Debug("Policy evaluation completed",
		log.String("event_id", event.ID),
		log.Any("duration", duration),
		log.Int("matches", len(results)))

	return results, nil
}

// Evaluate 评估单个策略
func (e *DefaultPolicyEngine) Evaluate(policyID string, event *common.Event) (bool, error) {
	startTime := time.Now()

	// 获取策略
	policy, err := e.store.Get(policyID)
	if err != nil {
		return false, fmt.Errorf("failed to get policy %s for event %s: %w", policyID, event.ID, err)
	}

	e.logger.Debug("Evaluating single policy",
		log.String("policy_id", policyID),
		log.String("event_id", event.ID))

	// 构建事件数据
	accessor := NewEventAccessor(event)
	eventData := accessor.ToMap()

	// 评估（使用 background context，不限制超时）
	match := e.evaluatePolicy(context.Background(), policy, eventData, startTime)

	if match.Error != nil {
		e.logger.Error("Policy evaluation failed",
			log.String("policy_id", policyID),
			log.String("event_id", event.ID),
			log.Error(match.Error))
		return false, match.Error
	}

	e.logger.Debug("Policy evaluation completed",
		log.String("policy_id", policyID),
		log.String("event_id", event.ID),
		log.Any("matched", match.Matched),
		log.Any("duration", time.Since(startTime)))

	return match.Matched, nil
}

// evaluatePolicy 评估单个策略
func (e *DefaultPolicyEngine) evaluatePolicy(ctx context.Context, policy *Policy, eventData map[string]interface{}, startTime time.Time) *PolicyMatch {
	evalStart := time.Now()

	// 获取编译后的程序
	program, err := e.manager.GetCompiledProgram(policy.ID)
	if err != nil {
		return &PolicyMatch{
			PolicyID:  policy.ID,
			Matched:   false,
			Error:     err,
			Expansion: policy.Expansion,
			Timeout:   policy.Timeout,
		}
	}

	// 评估表达式（带 context 支持超时和取消）
	matched, err := program.EvaluateWithCtx(ctx, eventData)
	if err != nil {
		return &PolicyMatch{
			PolicyID:  policy.ID,
			Matched:   false,
			Error:     err,
			Expansion: policy.Expansion,
			Timeout:   policy.Timeout,
		}
	}

	return &PolicyMatch{
		PolicyID:       policy.ID,
		Matched:        matched,
		EvaluationTime: time.Since(evalStart).Milliseconds(),
		Expansion:      policy.Expansion,
		Timeout:        policy.Timeout,
	}
}

// Start 启动策略引擎
func (e *DefaultPolicyEngine) Start(ctx context.Context) error {
	e.ctx, e.cancel = context.WithCancel(ctx)
	// 预编译所有策略
	if err := e.manager.ReloadPolicies(); err != nil {
		return fmt.Errorf("reload policies: %w", err)
	}

	return nil
}

// Init 初始化策略引擎，创建 DSL 引擎
// 策略仅通过 HTTP API 管理，不从文件加载
func (e *DefaultPolicyEngine) Init(cfg config.ConfigManager) error {
	// 默认值
	e.maxConcurrentEvals = 50

	// 读取 [policy] 配置
	policyConfig := cfg.Get("policy")
	policyMap, ok := policyConfig.(map[string]interface{})
	if !ok {
		e.logger.Info("No policy configuration found")
		return nil
	}

	// 读取最大并发评估数
	if v := cfg.GetInt("policy.max_concurrent_evaluations"); v > 0 {
		e.maxConcurrentEvals = v
		e.logger.Info("Max concurrent evaluations configured", log.Int("max", e.maxConcurrentEvals))
	}

	dslEngineType, _ := policyMap["type"].(string)
	if dslEngineType == "" {
		return fmt.Errorf("policy.type is required when [policy] section is configured")
	}
	e.logger.Info("Using DSL engine", log.String("type", dslEngineType))

	// 使用工厂创建 DSL 引擎
	engineConfig := &DSLEngineConfig{Type: dslEngineType}
	dslEngine, err := Factory.Create(engineConfig)
	if err != nil {
		return fmt.Errorf("create DSL engine: %w", err)
	}
	e.logger.Info("DSL engine created", log.String("type", dslEngine.GetType()))

	// 配置评估超时（仅对支持的引擎生效）
	if v := cfg.GetString("policy.evaluation_timeout"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			if setter, ok := dslEngine.(interface{ SetEvaluationTimeout(time.Duration) }); ok {
				setter.SetEvaluationTimeout(d)
				e.logger.Info("Evaluation timeout configured", log.String("timeout", v))
			}
		}
	}

	// 注册 DSL 引擎到管理器
	e.manager.RegisterEngine(dslEngine)
	e.logger.Info("DSL engine registered")

	return nil
}

// Stop 停止策略引擎
func (e *DefaultPolicyEngine) Stop() error {
	if e.cancel != nil {
		e.cancel()
	}
	return nil
}

// Health 健康检查
func (e *DefaultPolicyEngine) Health() error {
	// 检查策略存储
	_, err := e.store.Count()
	if err != nil {
		return fmt.Errorf("policy store health check failed: %w", err)
	}

	return nil
}

// GetManager 获取策略管理器
func (e *DefaultPolicyEngine) GetManager() PolicyManager {
	return e.manager
}

// updateStats 更新统计信息
func (e *DefaultPolicyEngine) updateStats(results []*PolicyMatch, duration time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.stats.TotalEvaluations += int64(len(results))

	for _, result := range results {
		if result.Matched {
			e.stats.TotalMatches++
		}
	}

	// 计算匹配率
	if e.stats.TotalEvaluations > 0 {
		e.stats.MatchRate = float64(e.stats.TotalMatches) / float64(e.stats.TotalEvaluations)
	}

	// 计算平均延迟
	if e.stats.TotalEvaluations > 0 {
		e.stats.AverageLatency = (e.stats.AverageLatency*(e.stats.TotalEvaluations-int64(len(results))) + duration.Nanoseconds()) / e.stats.TotalEvaluations
	}
}

// GetStats 获取统计信息
func (e *DefaultPolicyEngine) GetStats() EngineStats {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.stats
}

// ListPolicies 列出所有策略
func (e *DefaultPolicyEngine) ListPolicies() ([]*Policy, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	allPolicies, err := e.store.List()
	if err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}

	return allPolicies, nil
}

// GetPolicy 获取单个策略详情
func (e *DefaultPolicyEngine) GetPolicy(policyID string) (*Policy, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	policy, err := e.store.Get(policyID)
	if err != nil {
		return nil, fmt.Errorf("get policy: %w", err)
	}

	return policy, nil
}

// ResetStats 重置统计信息
func (e *DefaultPolicyEngine) ResetStats() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stats = EngineStats{}
}
