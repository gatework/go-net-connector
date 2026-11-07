package workflow

import (
	"context"

	"github.com/gatework/go-net-connector/connector"
	"github.com/gatework/go-net-connector/internal/progress"
)

func (r *Runner) reporter() *progress.Reporter {
	if r.Reporter == nil {
		r.Reporter = progress.New(r.Progress, false)
	}
	return r.Reporter
}
func (r *Runner) observe() SessionObserver {
	return func(_ context.Context, e connector.SessionEvent) error {
		_ = r.reporter().Event(e.Host, e.Name, e.Status)
		return nil
	}
}
func (r *Runner) finishBatch() {
	_ = r.reporter().Finish()
}
