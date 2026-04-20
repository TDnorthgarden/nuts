package event

import (
	"context"
	"fmt"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
	"github.com/nuts-project/nuts/pkg/storage"
)

// InfluxDBEventStore InfluxDB事件存储实现
type InfluxDBEventStore struct {
	client   influxdb2.Client
	writeAPI api.WriteAPI
	queryAPI api.QueryAPI
	org      string
	bucket   string
}

// NewInfluxDBEventStore 创建InfluxDB事件存储
func NewInfluxDBEventStore(url, token, org, bucket string) (storage.EventStore, error) {
	client := influxdb2.NewClient(url, token)

	// 验证连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := client.Health(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to InfluxDB: %w", err)
	}

	return &InfluxDBEventStore{
		client:   client,
		writeAPI: client.WriteAPI(org, bucket),
		queryAPI: client.QueryAPI(org),
		org:      org,
		bucket:   bucket,
	}, nil
}

// Write 写入单个事件
func (s *InfluxDBEventStore) Write(event *storage.Event) error {
	point := influxdb2.NewPoint("event",
		map[string]string{
			"id":        event.ID,
			"type":      event.Type,
			"cgroup_id": event.CgroupID,
			"policy_id": event.PolicyID,
		},
		event.Data,
		event.Timestamp,
	)

	s.writeAPI.WritePoint(point)
	return nil
}

// WriteBatch 批量写入事件
func (s *InfluxDBEventStore) WriteBatch(events []*storage.Event) error {
	for _, event := range events {
		if err := s.Write(event); err != nil {
			return err
		}
	}
	return nil
}

// Query 查询事件
func (s *InfluxDBEventStore) Query(query *storage.EventQuery) ([]*storage.Event, error) {
	// 构建Flux查询
	fluxQuery := s.buildFluxQuery(query)

	result, err := s.queryAPI.Query(context.Background(), fluxQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}

	var events []*storage.Event
	for result.Next() {
		event := &storage.Event{
			ID:        result.Record().ValueByKey("id").(string),
			Type:      result.Record().ValueByKey("type").(string),
			CgroupID:  result.Record().ValueByKey("cgroup_id").(string),
			PolicyID:  result.Record().ValueByKey("policy_id").(string),
			Timestamp: result.Record().Time(),
			Data:      make(map[string]interface{}),
		}

		// 复制所有字段到Data
		for key, value := range result.Record().Values() {
			if key != "_time" && key != "_measurement" && key != "id" && key != "type" && key != "cgroup_id" && key != "policy_id" {
				event.Data[key] = value
			}
		}

		events = append(events, event)

		if query.Limit > 0 && len(events) >= query.Limit {
			break
		}
	}

	if err := result.Err(); err != nil {
		return nil, err
	}

	return events, nil
}

// QueryByTimeRange 按时间范围查询事件
func (s *InfluxDBEventStore) QueryByTimeRange(start, end time.Time, filters map[string]string) ([]*storage.Event, error) {
	query := &storage.EventQuery{
		StartTime: start,
		EndTime:   end,
	}

	if cgroupID, ok := filters["cgroup_id"]; ok {
		query.CgroupID = cgroupID
	}
	if policyID, ok := filters["policy_id"]; ok {
		query.PolicyID = policyID
	}
	if eventType, ok := filters["event_type"]; ok {
		query.EventType = eventType
	}

	return s.Query(query)
}

// Delete 删除事件
func (s *InfluxDBEventStore) Delete(cgroupID string, policyID string) error {
	// InfluxDB不支持直接删除，需要使用delete API
	// 这里简化实现，实际应该使用delete API
	fluxQuery := fmt.Sprintf(`
		from(bucket: "%s")
			|> range(start: -30d)
			|> filter(fn: (r) => r.cgroup_id == "%s" and r.policy_id == "%s")
			|> drop(columns: ["_start", "_stop"])
	`, s.bucket, cgroupID, policyID)

	// 注意：InfluxDB 2.x的删除操作需要特殊权限
	// 这里只是示例，实际实现需要根据InfluxDB版本调整
	return fmt.Errorf("delete operation not implemented for InfluxDB")
}

// Close 关闭连接
func (s *InfluxDBEventStore) Close() error {
	s.writeAPI.Close()
	s.client.Close()
	return nil
}

// buildFluxQuery 构建Flux查询
func (s *InfluxDBEventStore) buildFluxQuery(query *storage.EventQuery) string {
	var fluxQuery string

	// 基础查询
	fluxQuery = fmt.Sprintf(`
		from(bucket: "%s")
			|> range(start: -30d)
	`, s.bucket)

	// 添加过滤条件
	if query.CgroupID != "" {
		fluxQuery += fmt.Sprintf(`|> filter(fn: (r) => r.cgroup_id == "%s")`, query.CgroupID)
	}
	if query.PolicyID != "" {
		fluxQuery += fmt.Sprintf(`|> filter(fn: (r) => r.policy_id == "%s")`, query.PolicyID)
	}
	if query.EventType != "" {
		fluxQuery += fmt.Sprintf(`|> filter(fn: (r) => r.type == "%s")`, query.EventType)
	}
	if !query.StartTime.IsZero() {
		fluxQuery += fmt.Sprintf(`|> filter(fn: (r) => r._time >= %s)`, query.StartTime.Format(time.RFC3339))
	}
	if !query.EndTime.IsZero() {
		fluxQuery += fmt.Sprintf(`|> filter(fn: (r) => r._time <= %s)`, query.EndTime.Format(time.RFC3339))
	}

	return fluxQuery
}
