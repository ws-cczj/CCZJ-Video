package matching

import "testing"

func TestChapterIdentityUsesAuthorCreditRules(t *testing.T) {
	if got := Identity("1984", "乔治·奥威尔", "1984", "【英】乔治·奥威尔 著,富强 译"); got != "verified" {
		t.Fatal(got)
	}
	if got := Identity("1984", "富强", "1984", "【英】乔治·奥威尔 著,富强 译"); got != "mismatch" {
		t.Fatal("translator became author", got)
	}
	if got := Identity("1984", "", "1984", "乔治·奥威尔"); got != "provisional" {
		t.Fatal(got)
	}
}
