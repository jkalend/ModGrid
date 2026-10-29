package server

import "testing"

func TestPropertiesGeneratedByMinecraft(t *testing.T) {
	input := []byte("#Minecraft server properties\nlevel-type=minecraft\\:normal\nserver-ip=127.0.0.1\nserver-port=25577\n")
	value, err := property(input, "server-ip")
	if err != nil || value != "127.0.0.1" {
		t.Fatal(value, err)
	}
	for _, b := range []string{"server\\u002dip=127.0.0.1", "foo=a\\\nserver-ip=127.0.0.1", "\u00a0server-ip=127.0.0.1", "server-ip=127.0.0.1\nserver-ip=0.0.0.0"} {
		if _, err := property([]byte(b), "server-ip"); err == nil {
			t.Fatalf("accepted ambiguous property %q", b)
		}
	}
	for _, b := range []string{"server-ip:127.0.0.1", "server-ip 127.0.0.1", "server-ip : 127.0.0.1"} {
		v, err := property([]byte(b), "server-ip")
		if err != nil || v != "127.0.0.1" {
			t.Fatal(v, err)
		}
	}
}
