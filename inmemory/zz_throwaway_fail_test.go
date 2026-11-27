package inmemory

import "testing"

func TestThrowawayProtectionCheck(t *testing.T) {
	t.Fatal("deliberate failure to test branch protection, do not merge")
}
