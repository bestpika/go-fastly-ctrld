package secrets

import "testing"

func TestRejectMalformedWithoutOpeningKeyring(t *testing.T) {
	for _, token := range []string{"", " secret", "secret\n", "secret\x00"} {
		if Validate(token) == nil {
			t.Fatal("未拒絕非法權杖")
		}
	}
	if Validate("secret-valid") != nil {
		t.Fatal("拒絕有效格式")
	}
}
