package project

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"drama-agent/internal/application/ports"
	taskapp "drama-agent/internal/application/task"
	"drama-agent/internal/domain/credits"
)

func TestPricedMockCandidatesKeepConfiguredPriceAndDoNotExecute(t *testing.T) {
	s, store, model := configFixture()
	c := configService(s)
	c.Tasks = &taskapp.Service{}
	ctx := context.Background()
	now := time.Now().UTC()
	start, end := now.Add(-time.Hour), now.Add(time.Hour)
	input := mockInput(1)
	input.Connection.Models[0].CreditPrice = &credits.PriceRule{UnitPrice: 5}
	input.Connection.Models[0].Description = "  本地图片效果测试  "
	input.Connection.Models[1].CreditPrice = &credits.PriceRule{UnitPrice: 8, FreeEnabled: true, FreeStart: &start, FreeEnd: &end}
	_, err := c.ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := store.ModelConfigSnapshot(ctx)
	options, err := c.UserOptions(ctx)
	if err != nil || len(options.Image) != 1 || len(options.Video) != 1 || len(options.Catalog) != 2 {
		t.Fatal("priced Mock candidates hidden", options, err)
	}
	if options.Image[0].CreditPrice.UnitPrice != 5 || options.Image[0].Free || options.Image[0].BillingUnit != "image" || options.Video[0].CreditPrice.UnitPrice != 8 || !options.Video[0].Free || options.Video[0].BillingUnit != "second" {
		t.Fatal("candidate price, promotion or unit lost", options)
	}
	if options.Image[0].Description != "本地图片效果测试" {
		t.Fatal("description lost during official resolution", options.Image[0])
	}
	after, _ := store.ModelConfigSnapshot(ctx)
	if !reflect.DeepEqual(before, after) || model.count != 0 {
		t.Fatal("candidate read changed configuration or called a model")
	}
	raw, _ := json.Marshal(before)
	for _, scenario := range []string{"disabled", "missing-price", "zero-price", "adapter-unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			if err := json.Unmarshal(raw, &store.cfg); err != nil {
				t.Fatal(err)
			}
			for i := range store.cfg.Connections {
				if store.cfg.Connections[i].ID != "mock" {
					continue
				}
				for j := range store.cfg.Connections[i].Models {
					m := &store.cfg.Connections[i].Models[j]
					switch scenario {
					case "disabled":
						m.Enabled = false
					case "missing-price":
						m.CreditPrice = nil
					case "zero-price":
						m.CreditPrice = &credits.PriceRule{}
					}
				}
			}
			c.Tasks = &taskapp.Service{}
			if scenario == "adapter-unavailable" {
				c.Tasks = nil
			}
			value, err := c.UserOptions(ctx)
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if scenario == "zero-price" {
				want = 1
			}
			if len(value.Image) != want || len(value.Video) != want {
				t.Fatal(scenario, value)
			}
		})
	}
}

func TestModelDescriptionValidationAndOfficialPreservation(t *testing.T) {
	for _, tc := range []struct {
		text  string
		valid bool
	}{
		{"", true}, {strings.Repeat("😀", 200), true}, {strings.Repeat("中", 201), false}, {"第一行\n第二行", false}, {"正文\x00尾", false}, {string([]byte{0xff}), false},
	} {
		s, _, _ := configFixture()
		in := mockInput(1)
		in.Connection.Models[0].Description = tc.text
		v, e := configService(s).ForOperator(ports.Actor{UserID: "fixture-operator", Role: ports.RoleOperator}).Update(context.Background(), in)
		if (e == nil) != tc.valid {
			t.Fatalf("description valid=%v: %v", tc.valid, e)
		}
		if e == nil {
			for _, conn := range v.Connections {
				if conn.ID == "mock" && conn.Models[0].Description != tc.text {
					t.Fatal("official metadata erased description")
				}
			}
		}
	}
}
