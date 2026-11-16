package domain

func KRXSymbol(symbol string) bool {
	if len(symbol) != 6 {
		return false
	}
	for i := range symbol {
		if symbol[i] < '0' || symbol[i] > '9' {
			return false
		}
	}
	return true
}
