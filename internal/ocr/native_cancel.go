package ocr

import "context"

// withNativeCancellation joins the interrupt callback before its caller can
// release native handles, including when run fails or panics. Interrupt must
// only signal cancellation; run owns the native operation until it returns.
func withNativeCancellation(ctx context.Context, interrupt func(), run func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(done)
		interrupt()
	})
	defer func() {
		if !stop() {
			<-done
		}
	}()
	err := run()
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	return err
}
