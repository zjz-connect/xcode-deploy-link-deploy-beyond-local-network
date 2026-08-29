package linkcore

import "testing"

func TestValidateCommand(t *testing.T) {
	accepted := []Request{
		{Command: "status"},
		{Command: "stop"},
		{Command: "install", AppPath: "/tmp/Test.app"},
	}
	for _, request := range accepted {
		if err := validateCommand(request); err != nil {
			t.Fatalf("request %#v: %v", request, err)
		}
	}
	rejected := []Request{
		{},
		{Command: "unknown"},
		{Command: "install"},
		{Command: "status", AppPath: "/tmp/Test.app"},
	}
	for _, request := range rejected {
		if err := validateCommand(request); errorCode(err) != "control_request_invalid" {
			t.Fatalf("request %#v error = %v", request, err)
		}
	}
}
