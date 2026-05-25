package eventbus

import (
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sig-cloudnative/nuts/api"
	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// GRPCEventBus gRPC实现的EventBus
type GRPCEventBus struct {
	// 服务端
	server   *grpc.Server
	listener net.Listener
	address  string

	// 客户端连接（用于发布）
	clientConn  *grpc.ClientConn
	eventClient api.EventBusServiceClient

	// 订阅管理
	subscribers map[string][]*Subscriber
	mu          sync.RWMutex

	// 客户端 gRPC 流订阅取消管理（topic -> cancel）
	grpcSubCancel   map[string]context.CancelFunc
	grpcSubCancelMu sync.Mutex

	// 序列化器
	serializer EventSerializer

	// 运行状态
	ctx    context.Context
	cancel context.CancelFunc

	// 日志记录器
	logger log.Logger

	// 丢弃事件计数（channel满时）
	dropCount atomic.Int64

	// 等待所有 goroutine 退出
	wg sync.WaitGroup
}

// Subscriber 订阅者信息
type Subscriber struct {
	ID       string
	Topic    string
	Ch       chan *common.Event
	Ctx      context.Context
	Cancel   context.CancelFunc
	IsRemote bool // 标识是否为远程订阅者（通过 gRPC 流）
	closed   sync.Once
}

// Close 安全关闭 channel
func (s *Subscriber) Close() {
	s.closed.Do(func() {
		close(s.Ch)
	})
}

// grpcServer 实现 EventBusService gRPC 服务
type grpcServer struct {
	api.UnimplementedEventBusServiceServer
	bus *GRPCEventBus
}

func (s *grpcServer) String() string {
	if s == nil {
		return "grpcServer(nil)"
	}
	if s.bus == nil {
		return "grpcServer{bus: nil}"
	}
	return "grpcServer{bus: initialized}"
}

// Publish 实现 gRPC Publish 方法
func (s *grpcServer) Publish(ctx context.Context, req *api.PublishRequest) (*api.PublishResponse, error) {
	defer func() {
		if r := recover(); r != nil {
			s.bus.logger.Error("Publish panic recovered", log.Any("panic", r))
		}
	}()

	if s == nil || s.bus == nil {
		return &api.PublishResponse{Success: false, Message: "server not initialized"}, nil
	}
	if s.bus.serializer == nil {
		return &api.PublishResponse{Success: false, Message: "serializer not initialized"}, nil
	}

	event, err := s.bus.serializer.Deserialize(req.EventData)
	if err != nil {
		return &api.PublishResponse{Success: false, Message: err.Error()}, nil
	}

	// 分发到本地订阅者
	s.bus.mu.RLock()
	subscribers := s.bus.subscribers[req.Topic]
	s.bus.mu.RUnlock()

	s.bus.logger.Info("Publish dispatching", log.Int("subscribers", len(subscribers)), log.String("topic", req.Topic))

	for _, sub := range subscribers {
		select {
		case sub.Ch <- event:
		case <-sub.Ctx.Done():
		}
	}

	return &api.PublishResponse{Success: true}, nil
}

// Subscribe 实现 gRPC Subscribe 方法（流式）
func (s *grpcServer) Subscribe(req *api.SubscribeRequest, stream api.EventBusService_SubscribeServer) error {
	if s == nil || s.bus == nil {
		return fmt.Errorf("server not initialized")
	}
	if s.bus.serializer == nil {
		return fmt.Errorf("serializer not initialized")
	}

	// 创建一个临时订阅者通道
	ch := make(chan *common.Event, 100)

	sub := &Subscriber{
		ID:       common.GenerateUUID(),
		Topic:    req.Topic,
		Ch:       ch,
		Ctx:      stream.Context(),
		Cancel:   func() {},
		IsRemote: true, // 标记为远程订阅者
	}

	s.bus.mu.Lock()
	if s.bus.subscribers == nil {
		s.bus.subscribers = make(map[string][]*Subscriber)
	}

	// 单订阅者保证：状态特定 topic 只允许一个订阅者
	if isStateSpecificTopic(req.Topic) {
		if existingSubs, exists := s.bus.subscribers[req.Topic]; exists && len(existingSubs) > 0 {
			// 关闭所有现有订阅者的 channel
			for _, existingSub := range existingSubs {
				existingSub.Close()
			}
			// 清空订阅列表
			s.bus.subscribers[req.Topic] = nil
		}
	}

	s.bus.subscribers[req.Topic] = append(s.bus.subscribers[req.Topic], sub)
	s.bus.mu.Unlock()

	defer func() {
		// 在函数退出时捕获 panic
		defer func() {
			if r := recover(); r != nil {
				s.bus.logger.Error("Subscribe panic recovered", log.Any("panic", r))
			}
		}()

		s.bus.mu.Lock()
		// 从订阅列表中移除
		if s.bus.subscribers != nil {
			subs := s.bus.subscribers[req.Topic]
			for i, subItem := range subs {
				if subItem.ID == sub.ID {
					s.bus.subscribers[req.Topic] = append(subs[:i], subs[i+1:]...)
					break
				}
			}
		}
		s.bus.mu.Unlock()

		// 尝试关闭 channel（使用 sync.Once 确保只关闭一次）
		sub.Close()
	}()

	// 循环发送事件到流
	for {
		select {
		case event := <-ch:
			if event == nil {
				return nil
			}
			data, err := s.bus.serializer.Serialize(event)
			if err != nil {
				continue
			}
			if err := stream.Send(&api.SubscribeResponse{EventData: data}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

// NewGRPCEventBusServer 创建gRPC EventBus服务端
func NewGRPCEventBusServer(address string, serializer EventSerializer) (*GRPCEventBus, error) {
	if serializer == nil {
		serializer = NewProtobufSerializer()
	}

	ctx, cancel := context.WithCancel(context.Background())

	bus := &GRPCEventBus{
		address:     address,
		subscribers: make(map[string][]*Subscriber),
		serializer:  serializer,
		ctx:         ctx,
		cancel:      cancel,
		logger:      log.GetDefault(),
		grpcSubCancel: make(map[string]context.CancelFunc),
	}

	// 创建gRPC服务器
	opts := []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle: 5 * time.Minute,
			Time:              2 * time.Hour,
			Timeout:           20 * time.Second,
		}),
	}

	bus.server = grpc.NewServer(opts...)

	// 注册 gRPC 服务实现
	gs := &grpcServer{bus: bus}
	if bus.serializer == nil {
		return nil, fmt.Errorf("serializer is nil after initialization")
	}
	api.RegisterEventBusServiceServer(bus.server, gs)

	return bus, nil
}

// NewGRPCEventBusClient 创建gRPC EventBus客户端
func NewGRPCEventBusClient(address string, serializer EventSerializer) (*GRPCEventBus, error) {
	if serializer == nil {
		serializer = NewProtobufSerializer()
	}

	addr, err := common.ParseAddr(address)
	if err != nil {
		return nil, fmt.Errorf("parse address: %w", err)
	}

	var (
		conn *grpc.ClientConn
	)
	ctx, cancel := context.WithCancel(context.Background())
	// 连接gRPC服务端
	switch addr.Scheme {
	case "tcp":
		conn, err = grpc.Dial(
			addr.Host,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
	case "unix":
		dialer := func(ctx context.Context, address string) (net.Conn, error) {
			return net.Dial("unix", addr.Host)
		}

		conn, err = grpc.Dial(
			addr.String(),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithContextDialer(dialer),
		)
	}

	if err != nil {
		cancel()
		return nil, fmt.Errorf("connect to grpc server: %w", err)
	}

	bus := &GRPCEventBus{
		address:     address,
		clientConn:  conn,
		eventClient: api.NewEventBusServiceClient(conn),
		subscribers: make(map[string][]*Subscriber),
		serializer:  serializer,
		ctx:         ctx,
		cancel:      cancel,
		logger:      log.GetDefault(),
		grpcSubCancel: make(map[string]context.CancelFunc),
	}

	return bus, nil
}

// SetLogger 设置日志记录器
func (b *GRPCEventBus) SetLogger(logger log.Logger) {
	b.logger = logger
}

// Start 启动 EventBus
// 服务端模式：启动 gRPC 服务器监听
// 客户端模式：连接已建立，无需额外操作
func (b *GRPCEventBus) Start(ctx context.Context) error {
	if b.server != nil {
		// 服务端模式：启动监听
		lis, err := common.NewListener(b.address)
		if err != nil {
			b.logger.Error("Failed to listen grpc", log.Error(err))
			return err
		}
		b.listener = lis

		b.wg.Add(1)
		go func() {
			defer b.wg.Done()
			if err := b.server.Serve(lis); err != nil {
				b.logger.Error("gRPC server error", log.Error(err))
			}
		}()
	}
	return nil
}

// Publish 发布事件
func (b *GRPCEventBus) Publish(topic string, event *common.Event) error {
	if b.eventClient != nil {
		// 客户端模式：通过gRPC发布到服务端
		return b.publishViaGRPC(topic, event)
	}

	// 服务端模式：本地分发
	return b.publishLocal(topic, event)
}

// publishViaGRPC 通过gRPC发布
func (b *GRPCEventBus) publishViaGRPC(topic string, event *common.Event) error {
	data, err := b.serializer.Serialize(event)
	if err != nil {
		return fmt.Errorf("serialize event: %w", err)
	}

	req := &api.PublishRequest{
		Topic:       topic,
		EventData:   data,
		ContentType: b.serializer.ContentType(),
	}

	ctx, cancel := context.WithTimeout(b.ctx, 5*time.Second)
	defer cancel()

	_, err = b.eventClient.Publish(ctx, req)
	if err != nil {
		return fmt.Errorf("grpc publish: %w", err)
	}

	return nil
}

// publishLocal 本地发布（服务端模式）
func (b *GRPCEventBus) publishLocal(topic string, event *common.Event) error {
	b.mu.RLock()
	subs := make([]*Subscriber, len(b.subscribers[topic]))
	copy(subs, b.subscribers[topic])
	b.mu.RUnlock()

	if len(subs) == 0 {
		return nil
	}

	// 异步分发到所有订阅者（使用 snapshot 避免 Unsubscribe 关闭 channel 时的竞争）
	for _, sub := range subs {
		select {
		case sub.Ch <- event:
		case <-sub.Ctx.Done():
			// 订阅者已取消，跳过
		case <-time.After(100 * time.Millisecond):
			count := b.dropCount.Add(1)
			if count <= 3 || count%1000 == 0 {
				b.logger.Warn("GRPCEventBus: event dropped due to slow subscriber",
					log.String("topic", topic),
					log.String("subscriber_id", sub.ID),
					log.Int("total_dropped", int(count)))
			}
		}
	}

	return nil
}

// Subscribe 订阅主题
func (b *GRPCEventBus) Subscribe(topic string) <-chan *common.Event {
	ch := make(chan *common.Event, 100)

	if b.eventClient != nil {
		// 客户端模式：建立gRPC流订阅，创建独立 subCtx 用于取消
		subCtx, cancel := context.WithCancel(b.ctx)
		b.grpcSubCancelMu.Lock()
		b.grpcSubCancel[topic] = cancel
		b.grpcSubCancelMu.Unlock()

		b.wg.Add(1)
		go b.subscribeViaGRPC(topic, ch, subCtx)
	} else {
		// 服务端模式：本地订阅
		b.subscribeLocal(topic, ch)
	}

	return ch
}

// subscribeViaGRPC 通过gRPC流订阅（带自动重连和指数退避）
func (b *GRPCEventBus) subscribeViaGRPC(topic string, ch chan *common.Event, subCtx context.Context) {
	defer close(ch)
	defer b.wg.Done()
	defer func() {
		b.grpcSubCancelMu.Lock()
		delete(b.grpcSubCancel, topic)
		b.grpcSubCancelMu.Unlock()
	}()

	retry := 0
	for {
		select {
		case <-subCtx.Done():
			return
		default:
		}

		req := &api.SubscribeRequest{
			Topic:    topic,
			ClientId: common.GenerateUUID(),
		}

		stream, err := b.eventClient.Subscribe(subCtx, req)
		if err != nil {
			b.logger.Warn("gRPC subscribe failed, retrying...",
				log.String("topic", topic),
				log.Int("retry", retry),
				log.Error(err))
			retry++
			select {
			case <-time.After(subBackoff(retry)):
			case <-subCtx.Done():
				return
			}
			continue
		}

		// 消费流直到断开
	streamLoop:
		for {
			select {
			case <-subCtx.Done():
				return
			default:
			}
			resp, err := stream.Recv()
			if err != nil {
				if err == io.EOF || subCtx.Err() != nil {
					return
				}
				b.logger.Warn("gRPC subscribe stream broken, reconnecting...",
					log.String("topic", topic),
					log.Int("retry", retry),
					log.Error(err))
				retry++
				select {
				case <-time.After(subBackoff(retry)):
				case <-subCtx.Done():
					return
				}
				break streamLoop
			}

			event, err := b.serializer.Deserialize(resp.EventData)
			if err != nil {
				continue
			}

			// 成功收到事件，重置重试计数
			retry = 0

			select {
			case ch <- event:
			case <-subCtx.Done():
				return
			}
		}
	}
}

// subBackoff 订阅重连指数退避：1s, 2s, 4s, ... 最大 60s
func subBackoff(retry int) time.Duration {
	n := 1 << uint(retry)
	if n > 60 {
		n = 60
	}
	return time.Duration(n) * time.Second
}

// subscribeLocal 本地订阅（服务端模式）
func (b *GRPCEventBus) subscribeLocal(topic string, ch chan *common.Event) {
	sub := &Subscriber{
		ID:       common.GenerateUUID(),
		Topic:    topic,
		Ch:       ch,
		Ctx:      b.ctx,
		Cancel:   func() {},
		IsRemote: false, // 标记为本地订阅者
	}

	b.mu.Lock()
	b.subscribers[topic] = append(b.subscribers[topic], sub)
	b.mu.Unlock()
}

// Unsubscribe 取消订阅
// 对本地订阅者：从 map 移除并关闭 channel
// 对客户端 gRPC 流订阅：取消 subCtx 使 subscribeViaGRPC goroutine 退出
func (b *GRPCEventBus) Unsubscribe(topic string) error {
	// 1. 取消本地订阅者
	b.mu.Lock()
	if subs, ok := b.subscribers[topic]; ok {
		for _, sub := range subs {
			sub.Close()
		}
		delete(b.subscribers, topic)
	}
	b.mu.Unlock()

	// 2. 取消客户端 gRPC 流订阅
	b.grpcSubCancelMu.Lock()
	if cancel, ok := b.grpcSubCancel[topic]; ok {
		cancel()
		delete(b.grpcSubCancel, topic)
	}
	b.grpcSubCancelMu.Unlock()

	return nil
}

// Stop 停止EventBus
func (b *GRPCEventBus) Stop() error {
	return b.Close()
}

// Close 关闭EventBus
func (b *GRPCEventBus) Close() error {
	b.cancel()

	// 关闭所有 gRPC 流订阅（让 subscribeViaGRPC goroutine 退出）
	b.grpcSubCancelMu.Lock()
	for topic, cancel := range b.grpcSubCancel {
		cancel()
		delete(b.grpcSubCancel, topic)
	}
	b.grpcSubCancelMu.Unlock()

	// 关闭所有订阅的 channel
	b.mu.Lock()
	for topic, subs := range b.subscribers {
		for _, sub := range subs {
			sub.Close()
		}
		delete(b.subscribers, topic)
	}
	b.mu.Unlock()

	if b.server != nil {
		// 使用 Stop 而不是 GracefulStop，避免等待活跃连接
		// server.Stop() 会自动关闭 listener，无需额外关闭
		b.server.Stop()
		b.server = nil
		b.listener = nil
	}

	var err error
	if b.clientConn != nil {
		err = b.clientConn.Close()
		b.clientConn = nil
	}

	if b.listener != nil {
		err = b.listener.Close()
		b.listener = nil
	}

	// 等待所有 goroutine 退出
	b.wg.Wait()

	return err
}

// Health 健康检查
func (b *GRPCEventBus) Health() error {
	if b.clientConn != nil {
		// 检查连接状态
		state := b.clientConn.GetState()
		if state != connectivity.Ready {
			return fmt.Errorf("connection not ready: %v", state)
		}
	}
	return nil
}

// HasSubscribers 检查是否有订阅者
func (b *GRPCEventBus) HasSubscribers(topic string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	subs, exists := b.subscribers[topic]
	if !exists {
		return false
	}
	return len(subs) > 0
}

// HasRemoteSubscribers 检查是否有远程订阅者（通过 gRPC 流）
func (b *GRPCEventBus) HasRemoteSubscribers(topic string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	subs, exists := b.subscribers[topic]
	if !exists {
		return false
	}

	for _, sub := range subs {
		if sub.IsRemote {
			return true
		}
	}
	return false
}

// HasAnyRemoteSubscribers 检查是否有任何远程订阅者
func (b *GRPCEventBus) HasAnyRemoteSubscribers() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, subs := range b.subscribers {
		for _, sub := range subs {
			if sub.IsRemote {
				return true
			}
		}
	}
	return false
}

// isStateSpecificTopic 判断是否为状态特定 topic
// 格式: task.state_changed_<state>
// 例如: task.state_changed_pending, task.state_changed_validating
func isStateSpecificTopic(topic string) bool {
	return strings.HasPrefix(topic, common.TaskEventTopicPrefix)
}

// 暂时注释掉与EventBus接口冲突的方法，后续重构

// 注册到工厂
func init() {
	Factory.Register(
		"grpc",
		ParseGRPCConfig,
		ValidateGRPCConfig,
		func(cfg interface{}) (EventBus, error) {
			grpcCfg, ok := cfg.(*GRPCConfig)
			if !ok {
				return nil, fmt.Errorf("invalid config type")
			}

			// 核心服务使用服务端模式
			return NewGRPCEventBusServer(grpcCfg.Address, nil)
		},
	)
}
