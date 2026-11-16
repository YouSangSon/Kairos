package domain

type OperationalSnapshot struct {
	SHA256                            string
	Orders, Terminal, Active, Pending int
}

func PaperOperationalDecision(snapshot OperationalSnapshot) (string, string) {
	if snapshot.Pending > 0 {
		return "DEGRADED", "unresolved_action"
	}
	if snapshot.Terminal == 0 {
		return "INSUFFICIENT", "no_terminal_sample"
	}
	return "PASS", "operationally_complete"
}

type paperOperationalSnapshot = OperationalSnapshot
