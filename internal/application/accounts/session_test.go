package accounts

import "testing"

func TestSessionEnvelopeRequiresPlatformBoundDigest(t *testing.T) {
	secret := RandomToken()
	_, workspace, wsDigest, err := SessionParts("user.workspace." + secret)
	if err != nil || workspace != Workspace || wsDigest != Digest("workspace:"+secret) {
		t.Fatal("workspace binding", err)
	}
	_, operations, opsDigest, err := SessionParts("user.operations." + secret)
	if err != nil || operations != Operations || opsDigest == wsDigest || opsDigest == Digest(secret) {
		t.Fatal("platform/legacy digest reused", err)
	}
	for _, value := range []string{"user." + secret, "user.other." + secret, ".workspace." + secret, "user.workspace.bad", "user.workspace." + secret + ".extra"} {
		if _, _, _, err := SessionParts(value); err == nil {
			t.Fatal("invalid credential envelope accepted")
		}
	}
}
