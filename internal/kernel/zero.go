package kernel

func FirstNonZero[T comparable](a, b T) T {
	var zero T
	if a != zero {
		return a
	}
	return b
}
