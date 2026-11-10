package application

import "context"

func writeResult[T any](ctx context.Context, journal UnitOfWork, work func(WriteScope) (T, error)) (T, error) {
	var result T
	err := journal.Write(ctx, func(scope WriteScope) error { var err error; result, err = work(scope); return err })
	if err != nil {
		var zero T
		return zero, err
	}
	return result, nil
}

func decideResult[T any](ctx context.Context, journal UnitOfWork, work func(WriteScope) (T, bool, error)) (T, error) {
	var result T
	err := journal.Decide(ctx, func(scope WriteScope) (bool, error) {
		var commit bool
		var err error
		result, commit, err = work(scope)
		return commit, err
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return result, nil
}
