package account

import "strconv"

func itoa(n int) string { return strconv.Itoa(n) }

func trimFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
