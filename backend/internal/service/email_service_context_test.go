//go:build unit

package service

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type smtpContextSettingRepo struct {
	SettingRepository
	port int
}

func (r *smtpContextSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{
		SettingKeySMTPHost: "127.0.0.1", SettingKeySMTPPort: fmt.Sprint(r.port),
		SettingKeySMTPUsername: "sender@example.com", SettingKeySMTPPassword: "password",
		SettingKeySMTPFrom: "sender@example.com", SettingKeySMTPUseTLS: "false",
	}, nil
}

func TestSendEmailContextCancellationClosesBlockedSMTPAndJoins(t *testing.T) {
	for _, phase := range []string{"greeting", "auth", "data"} {
		for _, deadline := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/deadline=%t", phase, deadline), func(t *testing.T) {
				listener, err := net.Listen("tcp", "127.0.0.1:0")
				require.NoError(t, err)
				defer func() { _ = listener.Close() }()
				reached, serverDone := make(chan struct{}), make(chan struct{})
				go func() {
					defer close(serverDone)
					conn, acceptErr := listener.Accept()
					if acceptErr != nil {
						return
					}
					defer func() { _ = conn.Close() }()
					_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
					reader := bufio.NewReader(conn)
					if phase == "greeting" {
						close(reached)
						_, _ = reader.ReadString('\n')
						return
					}
					_, _ = fmt.Fprint(conn, "220 local ESMTP ready\r\n")
					for {
						line, readErr := reader.ReadString('\n')
						if readErr != nil {
							return
						}
						command := strings.ToUpper(strings.TrimSpace(line))
						switch {
						case strings.HasPrefix(command, "EHLO"):
							_, _ = fmt.Fprint(conn, "250-local\r\n250 AUTH PLAIN\r\n")
						case strings.HasPrefix(command, "AUTH"):
							if phase == "auth" {
								close(reached)
								_, _ = reader.ReadString('\n')
								return
							}
							_, _ = fmt.Fprint(conn, "235 authentication successful\r\n")
						case strings.HasPrefix(command, "MAIL"), strings.HasPrefix(command, "RCPT"):
							_, _ = fmt.Fprint(conn, "250 ok\r\n")
						case command == "DATA":
							close(reached)
							_, _ = reader.ReadString('\n')
							return
						default:
							return
						}
					}
				}()
				address, ok := listener.Addr().(*net.TCPAddr)
				require.True(t, ok)
				svc := NewEmailService(&smtpContextSettingRepo{port: address.Port}, nil)
				var ctx context.Context
				var cancel context.CancelFunc
				if deadline {
					ctx, cancel = context.WithTimeout(context.Background(), 250*time.Millisecond)
				} else {
					ctx, cancel = context.WithCancel(context.Background())
				}
				defer cancel()
				done := make(chan error, 1)
				go func() { done <- svc.SendEmail(ctx, "recipient@example.com", "subject", "body") }()
				select {
				case <-reached:
				case sendErr := <-done:
					t.Fatalf("SMTP returned before reaching %s: %v", phase, sendErr)
				case <-time.After(time.Second):
					t.Fatalf("SMTP did not reach %s", phase)
				}
				if !deadline {
					cancel()
				}
				select {
				case sendErr := <-done:
					if deadline {
						require.ErrorIs(t, sendErr, context.DeadlineExceeded)
					} else {
						require.ErrorIs(t, sendErr, context.Canceled)
					}
				case <-time.After(time.Second):
					t.Fatal("SendEmail did not join after context cancellation")
				}
				select {
				case <-serverDone:
				case <-time.After(time.Second):
					t.Fatal("SMTP connection was not closed on cancellation")
				}
			})
		}
	}
}
