package log

import "context"

// CtxDimensions can propagate log dimensions inside a context object
type CtxDimensions struct {
}

// From returns the dimensions currently set inside a context object
func (c *CtxDimensions) From(ctx context.Context) []interface{} {
	existing := ctx.Value(c)
	if existing == nil {
		return []interface{}{}
	}
	return existing.([]interface{})
}

// Append returns a new context that appends vals as dimensions for logging
func (c *CtxDimensions) Append(ctx context.Context, vals ...interface{}) context.Context {
	if len(vals) == 0 {
		return ctx
	}
	if len(vals)%2 != 0 {
		panic("Expected even number of values")
	}
	existing := c.From(ctx)
	newArr := make([]interface{}, 0, len(existing)+len(vals))
	newArr = append(newArr, existing...)
	newArr = append(newArr, vals...)
	return context.WithValue(ctx, c, newArr)
}

// With returns a new Context object that appends the dimensions inside ctx with the context logCtx
func (c *CtxDimensions) With(ctx context.Context, log Logger) *Context {
	return NewContext(log).With(c.From(ctx)...)
}
