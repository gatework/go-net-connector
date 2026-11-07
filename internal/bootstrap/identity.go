package bootstrap

import (
	"encoding/json"

	"github.com/gatework/go-net-connector/netdisco"
)

type httpCacheIdentity struct {
	Source         string                  `json:"source"`
	URL            string                  `json:"url"`
	Authentication string                  `json:"authentication"`
	Username       string                  `json:"username,omitempty"`
	Namespace      string                  `json:"namespace,omitempty"`
	Query          httpInventoryQueryShape `json:"query"`
}

type httpInventoryQueryShape struct {
	Endpoint              string `json:"endpoint"`
	Fields                string `json:"fields"`
	PageSize              int    `json:"page_size"`
	FallbackQuery         string `json:"fallback_q"`
	FallbackSeeAllColumns bool   `json:"fallback_seeallcolumns"`
}

// httpSourceIdentity describes result-affecting HTTP source settings without
// persisting passwords or API keys in the cache identity material.
func httpSourceIdentity(options netdisco.HTTPOptions, namespace string) ([]byte, error) {
	identity := httpCacheIdentity{
		Source:         "http",
		URL:            options.URL,
		Authentication: "basic",
		Username:       options.Username,
		Namespace:      namespace,
		Query: httpInventoryQueryShape{
			Endpoint:              "api/v1/search/device",
			Fields:                "ip,name,dns,vendor,os,model,os_ver,serial",
			PageSize:              options.Limits.PageSize,
			FallbackQuery:         "%",
			FallbackSeeAllColumns: true,
		},
	}
	if options.APIKey != "" {
		identity.Authentication = "api_key"
	}
	return json.Marshal(identity)
}
