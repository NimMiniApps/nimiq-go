package rpc

import (
	"context"
	"testing"
)

func TestGetInherentsByBlockNumber(t *testing.T) {
	c, f := newFakeNode(t, map[string]string{
		"getInherentsByBlockNumber": `{"data":[` +
			`{"type":"reward","blockNumber":120,"blockTime":1725309410050,` +
			`"validatorAddress":"NQ83 BPY4 JPJH 5B1Y NYKQ 32V1 84L4 YU35 1VTL",` +
			`"target":"NQ46 Q7MG GCJK GTPC NR5A FHQL YCCF USGE JKYA",` +
			`"value":80271028,"hash":"558a"},` +
			`{"type":"jail","blockNumber":120,"blockTime":1725309410050,` +
			`"validatorAddress":"NQ20","target":"","value":0,"hash":"ec38"}` +
			`],"metadata":null}`,
	})
	inherents, err := c.GetInherentsByBlockNumber(context.Background(), 120)
	if err != nil {
		t.Fatal(err)
	}
	if len(inherents) != 2 {
		t.Fatalf("got %d inherents, want 2", len(inherents))
	}
	if !inherents[0].IsReward() {
		t.Error("inherents[0] should be a reward")
	}
	if inherents[1].IsReward() {
		t.Error("inherents[1] (jail) must not report as a reward")
	}
	if inherents[0].Value != 80271028 {
		t.Errorf("value = %d, want 80271028", inherents[0].Value)
	}
	if string(f.gotParams["getInherentsByBlockNumber"]) != `[120]` {
		t.Errorf("params = %s, want [120]", f.gotParams["getInherentsByBlockNumber"])
	}
}
