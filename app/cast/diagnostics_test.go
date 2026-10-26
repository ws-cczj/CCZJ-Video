package cast

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/operation"
)

func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "cczj-cast-test-")
	if err != nil {
		panic(err)
	}
	if err = applog.Init(directory); err != nil {
		panic(err)
	}
	code := m.Run()
	applog.Default().Close()
	_ = os.RemoveAll(directory)
	os.Exit(code)
}

func TestRendererReportsPlaybackAndProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := r.Header.Get("SOAPAction")
		var body string
		if strings.Contains(action, "GetTransportInfo") {
			body = `<u:GetTransportInfoResponse xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"><CurrentTransportState>PLAYING</CurrentTransportState><CurrentTransportStatus>OK</CurrentTransportStatus></u:GetTransportInfoResponse>`
		} else {
			body = `<u:GetPositionInfoResponse xmlns:u="urn:schemas-upnp-org:service:AVTransport:1"><RelTime>00:00:12.5</RelTime><TrackDuration>00:01:30</TrackDuration></u:GetPositionInfoResponse>`
		}
		fmt.Fprint(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>`+body+`</s:Body></s:Envelope>`)
	}))
	defer server.Close()
	svc := New(context.Background())
	svc.devices["tv"] = renderer{Device: Device{ID: "tv"}, endpoint: server.URL, service: "urn:schemas-upnp-org:service:AVTransport:1", seen: time.Now(), session: "session-test"}
	value, err := svc.StatusContext(context.Background(), "tv")
	if err != nil {
		t.Fatal(err)
	}
	if value.State != "PLAYING" || !value.HasPosition || value.Position != 12.5 || value.Duration != 90 {
		t.Fatalf("bad receiver state: %+v", value)
	}
}

func TestEmptyAcknowledgementDoesNotConfirmPlayback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><Response/></s:Body></s:Envelope>`)
	}))
	defer server.Close()
	svc := New(context.Background())
	svc.devices["tv"] = renderer{Device: Device{ID: "tv"}, endpoint: server.URL, service: "urn:schemas-upnp-org:service:AVTransport:1", seen: time.Now()}
	if _, err := svc.StatusContext(context.Background(), "tv"); err == nil {
		t.Fatal("accepted missing transport state as playback")
	}
}

func TestDiagnosticsCorrelateSOAPFaultWithoutURLSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		fmt.Fprint(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>714</errorCode><errorDescription>Illegal MIME-type</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`)
	}))
	defer server.Close()
	ctx, identity := operation.WithIdentity(context.Background(), "cast.test")
	sequence := applog.Default().LatestSeq()
	err := soap(ctx, renderer{Device: Device{ID: "fault-tv"}, endpoint: server.URL + "?token=must-not-appear", service: "urn:schemas-upnp-org:service:AVTransport:1", session: "cast-session"}, "Play", "")
	if err == nil {
		t.Fatal("fault accepted")
	}
	logs := ""
	for _, record := range applog.Default().Since(sequence, 0) {
		logs += record.Message + "\n"
	}
	for _, expected := range []string{identity.RequestID, "cast-session", "CAST_SOAP_FAULT", "714", "http_status", "response_sha256", "elapsed_ms"} {
		if !strings.Contains(logs, expected) {
			t.Fatalf("missing %s in %s", expected, logs)
		}
	}
	if strings.Contains(logs, "must-not-appear") {
		t.Fatal("logged secret query")
	}
}

func TestCastClockRejectsUnsupportedAndNonfiniteValues(t *testing.T) {
	for _, value := range []string{"NOT_IMPLEMENTED", "00:00:NaN", "00:99:01", "-1:00:00"} {
		if _, ok := clockSeconds(value); ok {
			t.Fatalf("accepted %s", value)
		}
	}
}
