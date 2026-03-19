package main

import "testing"

func TestGreet(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "Alice", want: "Hello, Alice!"},
		{name: "Bob", want: "Hello, Bob!"},
		{name: "", want: "Hello, World!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := greet(tt.name)
			if got != tt.want {
				t.Errorf("greet(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
