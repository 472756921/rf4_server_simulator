package store

import (
	"context"
	"time"
)

func contextWithTimeoutImpl(d time.Duration) (context.Context, func()) {
	return context.WithTimeout(context.Background(), d)
}

type ctxT = context.Context
