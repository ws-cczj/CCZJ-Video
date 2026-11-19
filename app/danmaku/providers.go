package danmaku

type providerDefinition struct {
	id           string
	seriesScoped bool
	validEpisode func(Episode) bool
	newProvider  func(*Client) Provider
	hosts        []string
}

// Order defines fallback priority. Each entry also owns the source's binding,
// cache identity and allowed request destinations.
var builtinProviders = [...]providerDefinition{
	{
		id: "bilibili", validEpisode: func(e Episode) bool {
			return validDecimalEpisode(e) && (e.AID == "" || decimalID.MatchString(e.AID))
		},
		newProvider: func(c *Client) Provider { return &bilibiliProvider{client: c} },
		hosts:       []string{"api.bilibili.com", "www.bilibili.com"},
	},
	{
		id: "qq", validEpisode: func(e Episode) bool { return qqID.MatchString(e.EpisodeID) },
		newProvider: func(c *Client) Provider { return &qqProvider{client: c} },
		hosts:       []string{"pbaccess.video.qq.com", "dm.video.qq.com"},
	},
	{
		id: "iqiyi", validEpisode: validDecimalEpisode,
		newProvider: func(c *Client) Provider { return &iqiyiProvider{client: c} },
		hosts:       []string{"mesh.if.iqiyi.com", "www.iqiyi.com", "pcw-api.iqiyi.com", "pcw-api.iq.com", "cmts.iqiyi.com"},
	},
	{
		id: mangoProviderID, seriesScoped: true, validEpisode: validDecimalSeriesEpisode,
		newProvider: func(c *Client) Provider { return newMangoProvider(c) },
		hosts:       []string{mangoSearchHost, mangoCatalogHost, mangoBarrageHost, mangoCDNAliHost, mangoCDNWSHost},
	},
	{
		id: miguProviderID, seriesScoped: true, validEpisode: validDecimalSeriesEpisode,
		newProvider: func(c *Client) Provider { return &miguProvider{client: c} },
		hosts:       []string{miguSearchHost, miguCatalogHost, miguBarrageHost},
	},
	{
		id: youkuProviderID, seriesScoped: true, validEpisode: func(e Episode) bool {
			return youkuShowID.MatchString(e.SeriesID) && youkuVideoID.MatchString(e.EpisodeID)
		},
		newProvider: func(c *Client) Provider { return newYoukuProvider(c) },
		hosts:       []string{youkuSearchHost, youkuCatalogHost, youkuMTOPHost},
	},
	{
		id: sohuProviderID, seriesScoped: true, validEpisode: validDecimalSeriesEpisode,
		newProvider: func(c *Client) Provider { return &sohuProvider{client: c} },
		hosts:       []string{sohuSearchHost, sohuCatalogHost, sohuBarrageHost},
	},
	{
		id: leshiProviderID, seriesScoped: true, validEpisode: validDecimalSeriesEpisode,
		newProvider: func(c *Client) Provider { return &leshiProvider{client: c} },
		hosts:       []string{leshiSearchHost, leshiCatalogHost, leshiVideoHost, leshiBarrageHost},
	},
	{
		id: xiguaProviderID, seriesScoped: true, validEpisode: validDecimalSeriesEpisode,
		newProvider: func(c *Client) Provider { return &xiguaProvider{client: c} },
		hosts:       []string{xiguaWebHost, xiguaBarrageHost},
	},
	{
		id: bahamutProviderID, seriesScoped: true, validEpisode: validDecimalSeriesEpisode,
		newProvider: func(c *Client) Provider { return &bahamutProvider{client: c} },
		hosts:       []string{bahamutAPIHost},
	},
}

func validDecimalEpisode(e Episode) bool {
	return decimalID.MatchString(e.EpisodeID)
}

func validDecimalSeriesEpisode(e Episode) bool {
	return decimalID.MatchString(e.SeriesID) && validDecimalEpisode(e)
}

func findProvider(name string) *providerDefinition {
	for i := range builtinProviders {
		if builtinProviders[i].id == name {
			return &builtinProviders[i]
		}
	}
	return nil
}

func seriesScopedProvider(name string) bool {
	source := findProvider(name)
	return source != nil && source.seriesScoped
}
