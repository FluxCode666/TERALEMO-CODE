package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestBuildGrokVoiceURL_UsesAPIDefaultForCLIProxyBase(t *testing.T) {
	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"base_url": xai.DefaultCLIBaseURL,
		},
	}
	url, err := buildGrokVoiceURL(account, nil, "tts")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/tts", url)

	url, err = buildGrokVoiceURL(account, nil, "realtime")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/realtime", url)
}

type grokRealtimeErrorUpstream struct {
	started atomic.Bool
	err     chan error
}

func (u *grokRealtimeErrorUpstream) ReadMessage(ctx context.Context) ([]byte, error) {
	if !u.started.Swap(true) {
		return []byte(`{"type":"response.audio.delta","delta":"YXVkaW8="}`), nil
	}
	select {
	case err := <-u.err:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (*grokRealtimeErrorUpstream) WriteJSON(context.Context, any) error { return nil }
func (*grokRealtimeErrorUpstream) Ping(context.Context) error           { return nil }
func (*grokRealtimeErrorUpstream) Close() error                         { return nil }

func TestGrokRealtimeAudioObservedSurvivesMalformedFrameAndAbnormalTermination(t *testing.T) {
	for _, scenario := range []string{"invalid JSON", "policy close", "upstream error"} {
		t.Run(scenario, func(t *testing.T) {
			upstreamErr := errors.New("upstream realtime connection failed")
			upstream := &grokRealtimeErrorUpstream{err: make(chan error, 1)}
			type relayResult struct {
				observed bool
				err      error
			}
			done := make(chan relayResult, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				client, err := coderws.Accept(w, r, nil)
				if err != nil {
					done <- relayResult{err: err}
					return
				}
				defer client.CloseNow()
				observed, err := (&OpenAIGatewayService{}).ProxyGrokRealtimeConn(r.Context(), nil, client, &GrokRealtimeUpstream{conn: upstream})
				done <- relayResult{observed: observed, err: err}
			}))
			t.Cleanup(server.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			require.NoError(t, err)
			defer client.CloseNow()
			_, audio, err := client.Read(ctx)
			require.NoError(t, err)
			require.True(t, grokRealtimeEventHasAudio(audio), "audio must be delivered before the failing event")
			switch scenario {
			case "invalid JSON":
				require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":`)))
			case "policy close":
				_ = client.Close(coderws.StatusPolicyViolation, "bad frame")
			case "upstream error":
				upstream.err <- upstreamErr
			}
			select {
			case result := <-done:
				require.True(t, result.observed)
				require.Error(t, result.err)
				switch scenario {
				case "invalid JSON":
					require.ErrorContains(t, result.err, "invalid realtime event")
				case "policy close":
					require.Equal(t, coderws.StatusPolicyViolation, coderws.CloseStatus(result.err))
				case "upstream error":
					require.ErrorIs(t, result.err, upstreamErr)
				}
			case <-ctx.Done():
				t.Fatal("relay did not finish after the failing event")
			}
		})
	}
}

func TestBuildGrokVoiceURL_EmptyBaseFallsBackToAPI(t *testing.T) {
	account := &Account{
		Platform:    PlatformGrok,
		Type:        AccountTypeOAuth,
		Credentials: map[string]any{},
	}
	url, err := buildGrokVoiceURL(account, nil, "stt")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/stt", url)
}

func TestBuildGrokVoiceURL_RequiresEndpoint(t *testing.T) {
	account := &Account{Platform: PlatformGrok, Type: AccountTypeOAuth}
	_, err := buildGrokVoiceURL(account, nil, "  ")
	require.Error(t, err)
}

func TestBuildGrokVoiceURL_EncodesCustomVoicePathSegments(t *testing.T) {
	account := &Account{Platform: PlatformGrok, Type: AccountTypeOAuth}
	got, err := buildGrokVoiceURL(account, nil, "custom-voices/nlbqfwie/audio")
	require.NoError(t, err)
	require.Equal(t, xai.DefaultBaseURL+"/custom-voices/nlbqfwie/audio", got)

	_, err = buildGrokVoiceURL(account, nil, "custom-voices/../audio")
	require.Error(t, err)
}

func TestForwardGrokVoice_RejectsNonGrok(t *testing.T) {
	svc := &OpenAIGatewayService{}
	_, err := svc.ForwardGrokVoice(context.Background(), nil, &Account{Platform: PlatformOpenAI}, "tts", []byte(`{}`), "application/json")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not supported")
}

func TestAwaitGrokRealtimeAudioObservedReadsFlagAfterRelayExits(t *testing.T) {
	errCh := make(chan error, 1)
	var observed atomic.Bool
	go func() {
		observed.Store(true)
		errCh <- io.EOF
	}()
	got, err := awaitGrokRealtimeAudioObserved(errCh, &observed)
	require.ErrorIs(t, err, io.EOF)
	require.True(t, got, "audioObserved must be read after the relay returns, not before <-errCh")
}

func TestGrokRealtimeEventHasAudio(t *testing.T) {
	require.False(t, grokRealtimeEventHasAudio([]byte(`{"type":"session.created"}`)))
	require.False(t, grokRealtimeEventHasAudio([]byte(`{"type":"response.audio_transcript.delta","delta":"hi"}`)))
	require.False(t, grokRealtimeEventHasAudio([]byte(`{"type":"response.audio.delta","delta":""}`)))
	require.True(t, grokRealtimeEventHasAudio([]byte(`{"type":"response.audio.delta","delta":"abc"}`)))
	require.True(t, grokRealtimeEventHasAudio([]byte(`{"type":"response.output_audio.delta","audio":"abc"}`)))
}

func TestForwardGrokVoice_RejectsUnknownEndpoint(t *testing.T) {
	svc := &OpenAIGatewayService{}
	_, err := svc.ForwardGrokVoice(context.Background(), nil, &Account{Platform: PlatformGrok}, "unknown", []byte(`{}`), "application/json")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported")
}
