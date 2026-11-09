package contracts

type ImportRow map[string]string
type ImportInput struct {
	FileSHA256 string
	Rows       []ImportRow
}
