package datasource

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sig-cloudnative/nuts/api"
	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/log"
)

type MockDataSource struct {
	config *BaseDataSourceConfig

	stats          DataSourceStats
	startTime      time.Time
	eventsReceived atomic.Int64
	eventsSent     atomic.Int64
	eventsDropped  atomic.Int64

	ownCtx  context.Context
	stop    context.CancelFunc
	wg      sync.WaitGroup
	started atomic.Bool
	readyCh chan struct{}
	readyOnce sync.Once

	eventCh       chan<- *common.Event
	eventInterval time.Duration
	eventTypes    []string

	logger log.Logger
}

type MockDataSourceConfig struct {
	BaseDataSourceConfig

	EventIntervalMs int      `toml:"event_interval_ms"`
	EventTypes      []string `toml:"event_types"`
}

func NewMockDataSource(cfg *MockDataSourceConfig) (*MockDataSource, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	eventInterval := time.Duration(cfg.EventIntervalMs) * time.Millisecond
	if eventInterval <= 0 {
		eventInterval = 5 * time.Second
	}

	if len(cfg.EventTypes) == 0 {
		cfg.EventTypes = []string{
			"ContainerStart",
			"ContainerStop",
			"ContainerUpdate",
			"PodCreated",
			"PodDeleted",
		}
	}

	logger := log.GetDefault()
	if logger == nil {
		logger, _ = log.NewZapLogger("info")
	}

	return &MockDataSource{
		config: &cfg.BaseDataSourceConfig,

		eventInterval: eventInterval,
		eventTypes:    cfg.EventTypes,
		logger:        logger,
		readyCh:       make(chan struct{}),
	}, nil
}

func (m *MockDataSource) SetLogger(logger log.Logger) {
	m.logger = logger
}

func (m *MockDataSource) ParseConfig(config map[string]interface{}) error {
	intervalMs := 5000
	if v, ok := config["event_interval_ms"].(int64); ok {
		intervalMs = int(v)
	} else if v, ok := config["event_interval_ms"].(int); ok {
		intervalMs = v
	}
	m.eventInterval = time.Duration(intervalMs) * time.Millisecond

	if types, ok := config["event_types"].([]interface{}); ok {
		m.eventTypes = make([]string, 0, len(types))
		for _, t := range types {
			if s, ok := t.(string); ok {
				m.eventTypes = append(m.eventTypes, s)
			}
		}
	}

	if name, ok := config["name"].(string); ok {
		m.config.Name = name
	}
	if dsType, ok := config["type"].(string); ok {
		m.config.Type = dsType
	}

	return nil
}

func (m *MockDataSource) Start(ctx context.Context, eventCh chan<- *common.Event) error {
	if !m.started.CompareAndSwap(false, true) {
		return fmt.Errorf("mock data source is already running")
	}

	m.ownCtx, m.stop = context.WithCancel(ctx)
	m.eventCh = eventCh
	m.startTime = time.Now()
	healthCheckInterval := m.config.HealthCheckInterval

	m.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				m.logger.Error("Mock generateEvents panic", log.Any("recover", r))
			}
		}()
		defer m.wg.Done()
		m.generateEvents()
	}()

	m.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				m.logger.Error("Mock healthCheckLoop panic", log.Any("recover", r))
			}
		}()
		defer m.wg.Done()
		m.healthCheckLoop(healthCheckInterval)
	}()

	m.readyOnce.Do(func() {
		close(m.readyCh)
	})

	return nil
}

func (m *MockDataSource) Stop() error {
	if !m.started.CompareAndSwap(true, false) {
		return nil
	}

	if m.stop != nil {
		m.stop()
	}

	waitCh := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
	case <-time.After(5 * time.Second):
		m.logger.Warn("Mock data source stop timeout, forcing exit")
	}

	return nil
}

func (m *MockDataSource) Ready() <-chan struct{} {
	return m.readyCh
}

func (m *MockDataSource) Health() error {
	if !m.started.Load() {
		return fmt.Errorf("datasource not connected")
	}
	return nil
}

func (m *MockDataSource) GetStats() *DataSourceStats {
	return &DataSourceStats{
		EventsReceived: m.eventsReceived.Load(),
		EventsSent:     m.eventsSent.Load(),
		EventsDropped:  m.eventsDropped.Load(),
		LastEventTime:  time.Now(),
		Connected:      m.started.Load(),
		Uptime:         time.Since(m.startTime),
	}
}

func (m *MockDataSource) generateEvents() {
	ticker := time.NewTicker(m.eventInterval)
	defer ticker.Stop()

	counter := 0
	typeIndex := 0

	for {
		select {
		case <-m.ownCtx.Done():
			return
		case <-ticker.C:
			m.eventsReceived.Add(1)

			eventType := m.eventTypes[typeIndex]
			typeIndex = (typeIndex + 1) % len(m.eventTypes)

			event := common.NewEvent(eventType, "mock.event", "mock-datasource")
			event.TypedPayload = &api.Event_Pod{
				Pod: &api.PodEventPayload{
					PodName:      fmt.Sprintf("mock-pod-%d", counter),
					PodNamespace: "default",
					Extensions:   map[string]string{"counter": fmt.Sprintf("%d", counter)},
				},
			}

			counter++

			select {
			case m.eventCh <- event:
				m.eventsSent.Add(1)
				m.logger.Debug("Generated event", log.String("type", eventType), log.Int("counter", counter), log.String("pod", fmt.Sprintf("mock-pod-%d", counter)))
			case <-m.ownCtx.Done():
				return
			default:
				m.eventsDropped.Add(1)
			}
		}
	}
}

func (m *MockDataSource) healthCheckLoop(healthCheckInterval time.Duration) {
	ticker := time.NewTicker(healthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.ownCtx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *MockDataSourceConfig) Validate() error {
	if err := c.BaseDataSourceConfig.Validate(); err != nil {
		return err
	}
	return nil
}

func (c *MockDataSourceConfig) GetType() string {
	return "mock"
}

func (c *MockDataSourceConfig) GetName() string {
	return c.Name
}
