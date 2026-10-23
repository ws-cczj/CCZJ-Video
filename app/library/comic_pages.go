package library

import "net/url"

// Only reviewed, source-scoped assets belong here. On 2026-10-22 this exact
// upstream image was inspected: it is the yellow merchandise lottery page in
// 一人之下 / 第2话, not story artwork. Do not infer ads from page order,
// dimensions, repeated images or generic filenames such as "18.webp".
func reviewedComicPromotion(source, image string) bool {
	origin, err := url.Parse(source)
	if err != nil || origin.Hostname() != "www.guazimanhua.com" {
		return false
	}
	asset, err := url.Parse(image)
	return err == nil && asset.Hostname() == "img.guazicdn.com" && asset.Path == "/th/comics/chapters/26/260501/2_18.webp"
}
