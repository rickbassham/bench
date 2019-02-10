package bench_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/rickbassham/bench"
)

func TestRunner(t *testing.T) {
	r := bench.NewRunner(2, 2*time.Second, 60*time.Millisecond, "https://www.google.com/?test=")

	results := r.Run()

	output, _ := json.Marshal(results)

	println(fmt.Sprintf("%s", string(output)))

	t.Fail()
}
