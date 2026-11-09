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
