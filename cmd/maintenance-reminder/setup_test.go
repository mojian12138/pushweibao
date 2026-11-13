package main

import (
	"bufio"
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestListenOnPortRejectsInvalidAndUsesCustom(t *testing.T) {
	for _, value := range []string{"abc", "1", "65536", "-2"} {
		if _, err := listenOnPort(value); err == nil {
			t.Fatalf("expected invalid port %q", value)
		}
	}
	probe, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	listener, err := listenOnPort(strconv.Itoa(port))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if listener.Addr().(*net.TCPAddr).Port != port {
		t.Fatalf("bound %v, want port %d", listener.Addr(), port)
	}
	if !strings.HasPrefix(listener.Addr().String(), "0.0.0.0:") {
		t.Fatalf("unexpected bind address: %v", listener.Addr())
	}
}

func TestRecommendListenerUsesHighPort(t *testing.T) {
	listener, err := recommendListener()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	if port < portRangeStart || port > portRangeEnd {
		t.Fatalf("recommended port outside range: %d", port)
	}
}

func TestChooseListenerAcceptsRecommendationAndKeepsItBound(t *testing.T) {
	var out strings.Builder
	listener, err := chooseListener(bufio.NewReader(strings.NewReader("\n")), &out)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if !strings.Contains(out.String(), "推荐端口") {
		t.Fatalf("missing prompt: %s", out.String())
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if port < portRangeStart || port > portRangeEnd {
		t.Fatalf("port outside recommendation range: %d", port)
	}
	if _, err := net.Listen("tcp4", listener.Addr().String()); err == nil {
		t.Fatal("selected port was not kept reserved")
	}
}

func TestChooseListenerRetriesBusyCustomPort(t *testing.T) {
	busy, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port
	customProbe, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	custom := customProbe.Addr().(*net.TCPAddr).Port
	customProbe.Close()
	input := strings.NewReader(strconv.Itoa(port) + "\n" + strconv.Itoa(custom) + "\n")
	var out strings.Builder
	chooser := portChooser{Recommend: func() (net.Listener, error) { return net.Listen("tcp4", "0.0.0.0:0") }, Listen: listenOnPort}
	listener, err := chooser.choose(bufio.NewReader(input), &out)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if listener.Addr().(*net.TCPAddr).Port != custom {
		t.Fatalf("got %v, want custom %d", listener.Addr(), custom)
	}
	if !strings.Contains(out.String(), "端口不可用") {
		t.Fatalf("busy port was not reported: %s", out.String())
	}
}
