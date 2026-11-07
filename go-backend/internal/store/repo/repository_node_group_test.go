package repo

import (
	"encoding/json"
	"testing"

	"go-backend/internal/store/model"
)

func TestLegacySubscriptionBecomesItsOwnNodeGroup(t *testing.T) {
	r, err := Open(t.TempDir() + "/groups.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })

	// A subscription as an older version saved it: sources and node
	// processing on the subscription itself, the chain taking a source.
	legacy := `{"rename":"花云 =>","addFlag":true,"regions":["🇭🇰 香港"],"script":"x",` +
		`"chainRelaySources":[7],"chainLandingSources":[5],"rules":true,"templates":{"clash":3}}`
	if err := r.DB().Exec(`INSERT INTO subscription(name, kind, token, source_all, inbound_ids, source_ids, options, enabled, created_time, updated_time)
		VALUES('旧订阅', 'share', 'tok-legacy', 0, '11,12', '5', ?, 1, 1, 1)`, legacy).Error; err != nil {
		t.Fatal(err)
	}

	subs, err := r.ListSubscriptions()
	if err != nil {
		t.Fatal(err)
	}
	var s model.Subscription
	for _, x := range subs {
		if x.Token == "tok-legacy" {
			s = x
		}
	}
	ids := ParseGroupIDs(s.GroupIDs)
	if len(ids) != 1 {
		t.Fatalf("one group of its own: %q", s.GroupIDs)
	}
	g, err := r.GetNodeGroup(ids[0])
	if err != nil || g == nil {
		t.Fatal(err)
	}
	if g.Name != "旧订阅" || g.SourceAll != 0 || g.InboundIDs != "11,12" || g.SourceIDs != "5,7" {
		t.Fatalf("group sources (the chain's source joins once): %+v", g)
	}
	var opts map[string]interface{}
	_ = json.Unmarshal([]byte(g.Options), &opts)
	if opts["rename"] != "花云 =>" || opts["addFlag"] != true || opts["script"] != "x" || len(opts) != 4 {
		t.Fatalf("only the node processing moves: %v", opts)
	}
	// The subscription keeps its columns for a downgrade.
	if s.SourceIDs != "5" || s.InboundIDs != "11,12" || s.Options != legacy {
		t.Fatalf("legacy columns kept: %+v", s)
	}
	// Running again changes nothing.
	if err := r.GroupLegacySubscriptions(); err != nil {
		t.Fatal(err)
	}
	if groups, _ := r.ListNodeGroups(); len(groups) != 2 { // 我的订阅's group and this one
		t.Fatalf("groups: %d", len(groups))
	}
}
