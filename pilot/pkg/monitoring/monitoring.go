package monitoring

import (
	"context"

	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

// Int64...
type Int64 struct {
	*stats.Int64Measure

	tags []tag.Mutator
}

func (m *Int64) WithTags(tags ...tag.Mutator) *Int64 {
	return &Int64{m.Int64Measure, append(m.tags, tags...)}
}

func (m *Int64) Increment() {
	stats.RecordWithTags(context.Background(), m.tags, m.M(1))
}

func (m *Int64) Record(value int64) {
	stats.RecordWithTags(context.Background(), m.tags, m.M(value))
}

func NewInt64AndView(name, description string, aggregation *view.Aggregation, tagKeys ...tag.Key) (*Int64, *view.View) {
	i := &Int64{stats.Int64(name, description, stats.UnitDimensionless), make([]tag.Mutator, 0, len(tagKeys))}
	v := &view.View{Measure: i.Int64Measure, TagKeys: tagKeys, Aggregation: aggregation}
	return i, v
}

// Float64...
type Float64 struct {
	*stats.Float64Measure

	tags []tag.Mutator
}

func NewFloat64AndView(name, description string, aggregation *view.Aggregation, tagKeys ...tag.Key) (*Float64, *view.View) {
	f := &Float64{stats.Float64(name, description, stats.UnitDimensionless), make([]tag.Mutator, 0, len(tagKeys))}
	v := &view.View{Measure: f.Float64Measure, TagKeys: tagKeys, Aggregation: aggregation}
	return f, v
}

func (m *Float64) WithTags(tags ...tag.Mutator) *Float64 {
	return &Float64{m.Float64Measure, append(m.tags, tags...)}
}

func (m *Float64) Increment() {
	stats.RecordWithTags(context.Background(), m.tags, m.M(1))
}

func (m *Float64) Record(value float64) {
	stats.RecordWithTags(context.Background(), m.tags, m.M(value))
}
