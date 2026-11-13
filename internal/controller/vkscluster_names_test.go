package controller

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	virtfoundryv1alpha1 "github.com/virtfoundry/vks/api/v1alpha1"
)

func TestTCPNamespaceFor_Injective(t *testing.T) {
	mk := func(ns, name string) *virtfoundryv1alpha1.VKSCluster {
		return &virtfoundryv1alpha1.VKSCluster{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
	}
	a := tcpNamespaceFor(mk("virtfoundry-tenant-a-b", "c"))
	b := tcpNamespaceFor(mk("virtfoundry-tenant-a", "b-c"))
	if a == b {
		t.Fatalf("a-b/c and a/b-c collide on %q", a)
	}
	if long := tcpNamespaceFor(mk("virtfoundry-tenant-t", strings.Repeat("x", 80))); len(long) > 63 {
		t.Fatalf("namespace %q exceeds 63 chars", long)
	}
}

func TestJoinTokenFresh(t *testing.T) {
	now := time.Now()
	cases := map[string]struct {
		in   string
		want bool
	}{
		"missing":       {"", false},
		"garbage":       {"x", false},
		"expired":       {now.Add(-time.Minute).Format(time.RFC3339), false},
		"within margin": {now.Add(30 * time.Minute).Format(time.RFC3339), false},
		"fresh":         {now.Add(12 * time.Hour).Format(time.RFC3339), true},
	}
	for name, c := range cases {
		if got := joinTokenFresh([]byte(c.in), now); got != c.want {
			t.Errorf("%s: got %v want %v", name, got, c.want)
		}
	}
}
