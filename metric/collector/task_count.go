package collector

import (
	"context"

	metricAPI "github.com/bytebase/bytebase/metric"
	"github.com/bytebase/bytebase/plugin/metric"
	"github.com/bytebase/bytebase/store"
)

var _ metric.Collector = (*taskCountCollector)(nil)

// taskCountCollector is the metric data collector for task.
type taskCountCollector struct {
	store *store.Store
}

// NewTaskCountCollector creates a new instance of taskCollector.
func NewTaskCountCollector(store *store.Store) metric.Collector {
	return &taskCountCollector{
		store: store,
	}
}

// Collect will collect the metric for task.
func (c *taskCountCollector) Collect(ctx context.Context) ([]*metric.Metric, error) {
	var res []*metric.Metric

	taskCountMetricList, err := c.store.CountTaskGroupByTypeAndStatus(ctx)
	if err != nil {
		return nil, err
	}

	for _, taskCountMetric := range taskCountMetricList {
		res = append(res, &metric.Metric{
			Name:  metricAPI.TaskCountMetricName,
			Value: taskCountMetric.Count,
			Labels: map[string]interface{}{
				"type":   string(taskCountMetric.Type),
				"status": string(taskCountMetric.Status),
			},
		})
	}
	return res, nil
}
