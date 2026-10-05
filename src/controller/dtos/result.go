package dtos

type Result[T any] struct {
	Data T
	Err  error
}

func Ok[T any](data T) Result[T] {
	return Result[T]{Data: data}
}

func Fail[T any](err error) Result[T] {
	return Result[T]{Err: err}
}
