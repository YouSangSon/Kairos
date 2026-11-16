package strategydomain

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPaperProposalSMAVerifierMatchesPythonBoundedInputs(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("python3", "-c", `import json,random
from decimal import Decimal
from omni_research.improve import Parameters,sma_crossover
r=random.Random(38)
cases=[]
for fast,slow in [(1,2),(2,3),(3,6),(364,365)]:
 for mode in range(8):
  base=Decimal('999999999999') if mode%2 else Decimal('0.00001')
  closes=[base+Decimal(r.randrange(100))/Decimal(100000000) for _ in range(slow+1)]
  cases.append({'closes':[format(x,'f').rstrip('0').rstrip('.') if '.' in format(x,'f') else format(x,'f') for x in closes],'fast':fast,'slow':slow,'signal':sma_crossover(closes,slow,Parameters(fast,slow))})
print(json.dumps(cases))`)
	command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1", "PYTHONPATH="+filepath.Join(root, "services/research"))
	raw, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Python parity corpus: %v %s", err, raw)
	}
	var cases []struct {
		Closes     []string
		Fast, Slow int64
		Signal     string
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		if err := VerifySMACrossover(c.Closes, c.Fast, c.Slow, c.Signal); err != nil {
			t.Fatalf("parity case %d: %v", i, err)
		}
	}
}
