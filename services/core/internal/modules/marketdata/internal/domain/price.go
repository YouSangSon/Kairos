package domain

func OwnerPriceAdmitted(identities map[string]struct{}, instrument, currency string) bool {
	_, ok := identities[instrument+"\x00"+currency]
	return ok
}
