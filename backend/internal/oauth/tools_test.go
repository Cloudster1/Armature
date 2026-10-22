package oauth

import (
	"reflect"
	"testing"
)

func TestToolsOf(t *testing.T) {
	stator := ConnectedTool{Product: "stator", Name: "Stator", URL: "https://stator.example.com"}
	exciter := ConnectedTool{Product: "exciter", Name: "Exciter", URL: "https://exciter.example.com"}
	cases := []struct {
		name    string
		clients []Client
		want    []ConnectedTool
	}{
		{"none", nil, []ConnectedTool{}},
		{"both, Stator first whatever the order", []Client{
			{Name: "Exciter", ClientURI: exciter.URL, Status: StatusActive},
			{Name: "Stator", ClientURI: stator.URL, Status: StatusActive},
		}, []ConnectedTool{stator, exciter}},
		{"one", []Client{{Name: "Exciter", ClientURI: exciter.URL, Status: StatusActive}}, []ConnectedTool{exciter}},
		{"by the realm's client id, whatever the name", []Client{
			{Name: "Our wiki", ClientURI: stator.URL, KeycloakClientID: "stator", Status: StatusActive},
		}, []ConnectedTool{stator}},
		{"the name in any case", []Client{{Name: " stator ", ClientURI: stator.URL, Status: StatusActive}}, []ConnectedTool{stator}},
		{"a name that only starts like a tool's is another app", []Client{
			{Name: "Stator staging", ClientURI: stator.URL, KeycloakClientID: "stator-kc", Status: StatusActive},
		}, []ConnectedTool{}},
		{"without an address there is nowhere to go", []Client{{Name: "Stator", Status: StatusActive}}, []ConnectedTool{}},
		{"a pending app is not connected yet", []Client{{Name: "Stator", ClientURI: stator.URL, Status: StatusPending}}, []ConnectedTool{}},
		{"the first of several wins", []Client{
			{Name: "Stator", ClientURI: "https://new.example.com", Status: StatusActive},
			{Name: "Wiki", ClientURI: stator.URL, KeycloakClientID: "stator", Status: StatusActive},
		}, []ConnectedTool{{Product: "stator", Name: "Stator", URL: "https://new.example.com"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := toolsOf(c.clients); !reflect.DeepEqual(got, c.want) {
				t.Errorf("toolsOf = %+v, want %+v", got, c.want)
			}
		})
	}
}
