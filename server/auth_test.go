package server

import "testing"

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("hunter22")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(h, "hunter22") || VerifyPassword(h, "hunter23") || VerifyPassword("garbage", "hunter22") {
		t.Fatal("verify")
	}
	if _, err := HashPassword("abc"); err == nil {
		t.Fatal("short password accepted")
	}
}
