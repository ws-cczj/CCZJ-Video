package collect

import (
	"context"
	"testing"

	"cczjVideo/app/apperror"
)

func TestDetailSelectsRequestedIDInsteadOfFirstResponseRow(t *testing.T) {
	for _, config := range []string{"", `{"version":2,"strategy":"declarative","response":{"list_path":"data.items"}}`} {
		t.Run(config, func(t *testing.T) {
			body := `[{"vod_id":"other","vod_name":"双面游戏"},{"vod_id":"wanted","vod_name":"凡人修仙传","vod_play_url":"正片$https://media.example/video.m3u8"}]`
			if config == "" {
				body = `{"code":1,"list":` + body + `}`
			} else {
				body = `{"code":1,"data":{"items":` + body + `}}`
			}
			withStubBody(t, body)
			strategy := strategyFromJSON(t, "https://source.example/api", config)
			video, err := FetchVideoDetailWithStrategy(context.Background(), strategy, "wanted")
			if err != nil || video.VodName != "凡人修仙传" || video.VodId.String() != "wanted" {
				t.Fatalf("wrong detail: %+v %v", video, err)
			}
		})
	}
}

func TestDetailRejectsResponseWithNoRequestedID(t *testing.T) {
	withStubBody(t, `{"code":1,"list":[{"vod_id":"other","vod_name":"双面游戏"}]}`)
	strategy := strategyFromJSON(t, "https://source.example/api", "")
	video, err := FetchVideoDetailWithStrategy(context.Background(), strategy, "wanted")
	if video != nil || !apperror.Is(err, apperror.Corrupt) {
		t.Fatalf("wrong detail accepted: %+v %v", video, err)
	}
}
