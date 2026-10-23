// The probe is an isolated WebView2 host: it never opens the production database.
package main

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"

	"cczjVideo/app/resource"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func main() {
	renderOffscreen := os.Getenv("CCZJ_PROBE_RENDER_OFFSCREEN") == "1"
	browserArgs := []string{"--remote-debugging-address=127.0.0.1", "--remote-debugging-port=" + os.Args[3], "--autoplay-policy=no-user-gesture-required", "--disable-background-timer-throttling"}
	if renderOffscreen {
		browserArgs = append(browserArgs, "--disable-renderer-backgrounding", "--disable-backgrounding-occluded-windows", "--disable-features=CalculateNativeWinOcclusion")
	}
	if value := os.Getenv("CCZJ_PROBE_EXTRA_BROWSER_ARGS"); value != "" {
		var extra []string
		if err := json.Unmarshal([]byte(value), &extra); err != nil {
			panic(err)
		}
		browserArgs = append(browserArgs, extra...)
	}
	app := application.New(application.Options{Name: "CCZJ Playback Resource Probe", Windows: application.WindowsOptions{
		WebviewUserDataPath: os.Args[2], AdditionalBrowserArgs: browserArgs,
	}})
	app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "probe", Title: "CCZJ Playback Resource Probe", Width: 1280, Height: 800, URL: os.Args[1], Hidden: !renderOffscreen,
		InitialPosition: application.WindowXY, X: -16000, Y: -16000, Windows: application.WindowsWindow{HiddenOnTaskbar: true},
	})
	file, err := os.Create(os.Args[4])
	if err != nil {
		panic(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		var sampler resource.Sampler
		encoder := json.NewEncoder(file)
		for {
			if _, err := os.Stat(os.Args[4] + ".stop"); err == nil {
				app.Quit()
				return
			}
			if err := encoder.Encode(sampler.Sample()); err != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	// Bounds the helper's lifetime even if its test runner is interrupted.
	time.AfterFunc(12*time.Minute, app.Quit)
	err = app.Run()
	cancel()
	workers.Wait()
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		panic(err)
	}
}
