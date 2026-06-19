package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	sysloglistener "github.com/timmersuk/logthing/internal/syslog"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	network := flag.String("network", "udp", "network to use: udp or tcp")
	address := flag.String("addr", "127.0.0.1:5514", "syslog destination host:port")
	message := flag.String("message", sysloglistener.DefaultTestMessage, "message body")
	hostname := flag.String("hostname", "", "RFC5424 hostname")
	appName := flag.String("app", "logthing-sender", "RFC5424 app name")
	procID := flag.String("procid", fmt.Sprintf("%d", os.Getpid()), "RFC5424 process ID")
	msgID := flag.String("msgid", "manual-test", "RFC5424 message ID")
	facility := flag.Int("facility", 1, "syslog facility")
	severity := flag.Int("severity", 5, "syslog severity")
	timeout := flag.Duration("timeout", 5*time.Second, "send timeout")
	flag.Parse()

	sender, err := sysloglistener.NewSender(*network, *address, *timeout)
	if err != nil {
		return err
	}

	event := sysloglistener.Event{
		Timestamp: time.Now().UTC(),
		Hostname:  *hostname,
		AppName:   *appName,
		ProcID:    *procID,
		MsgID:     *msgID,
		Facility:  *facility,
		Severity:  *severity,
		Message:   *message,
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	result, err := sender.Send(ctx, event)
	if err != nil {
		return err
	}
	fmt.Printf("sent %s syslog message to %s\n", result.Network, result.Address)
	return nil
}
