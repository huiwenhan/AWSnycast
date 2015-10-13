package config

import (
	"testing"
)

func TestLoadConfig(t *testing.T) {
	c, err := New("../tests/awsnycast.yaml")
	if err != nil {
		t.Log(err)
		t.Fail()
	}
	if c == nil {
		t.Fail()
	}
}

func TestLoadConfigFails(t *testing.T) {
	_, err := New("../tests/doesnotexist.yaml")
	if err == nil {
		t.Fail()
	}
}

func TestLoadConfigHealthchecks(t *testing.T) {
	c, _ := New("../tests/awsnycast.yaml")
	if c.Healthchecks == nil {
		t.Fail()
	}
	h, ok := c.Healthchecks["public"]
	if !ok {
		t.Fail()
	}
	if h.Type != "ping" {
		t.Fail()
	}
	if h.Destination != "8.8.8.8" {
		t.Fail()
	}
	if h.Rise != 2 {
		t.Fail()
	}
	if h.Fall != 10 {
		t.Fail()
	}
	if h.Every != 1 {
		t.Fail()
	}
	a, ok := c.RouteTables["a"]
	if !ok {
		t.Log("RouteTables a not ok")
		t.Fail()
	}
	if a.Find.Type != "by_tag" {
		t.Fail()
	}
	if v, ok := a.Find.Config["key"]; ok {
		if v != "Name" {
			t.Fail()
		}
	} else {
		t.Fail()
	}
	if v, ok := a.Find.Config["value"]; ok {
		if v != "private a" {
			t.Fail()
		}
	} else {
		t.Fail()
	}
	routes := a.UpsertRoutes
	if len(routes) != 2 {
		t.Fail()
	}
	for _, route := range routes {
		if route.Cidr == "0.0.0.0/0" || route.Cidr == "192.168.1.1/32" {
			if route.Instance != "SELF" {
				t.Fail()
			}
			if route.Cidr == "0.0.0.0/0" {
				if route.Healthcheck != "public" {
					t.Fail()
				}
			} else {
				if route.Healthcheck != "localservice" {
					t.Fail()
				}
			}
		} else {
			t.Fail()
		}
	}
	b, ok := c.RouteTables["b"]
	if !ok {
		t.Fail()
	}
	if b.Find.Type != "by_tag" {
		t.Fail()
	}
}

func TestHealthcheckDefault(t *testing.T) {
	h := Healthcheck{
		Type: "ping",
	}
	h.Default()
	if h.Rise != 2 {
		t.Fail()
	}
	if h.Fall != 3 {
		t.Fail()
	}
}

func TestHealthcheckValidate(t *testing.T) {
	h := Healthcheck{
		Type: "ping",
	}
	h.Default()
	err := h.Validate()
	if err != nil {
		t.Log(err)
		t.Fail()
	}
}

func TestHealthcheckValidateFailType(t *testing.T) {
	h := Healthcheck{
		Type: "notping",
	}
	err := h.Validate()
	if err == nil {
		t.Fail()
	}
	if err.Error() != "Unknown healthcheck type 'notping'" {
		t.Log(err.Error())
		t.Fail()
	}
}

func TestHealthcheckValidateFailRise(t *testing.T) {
	h := Healthcheck{
		Type: "ping",
		Fall: 1,
	}
	err := h.Validate()
	if err == nil {
		t.Fail()
	}
	if err.Error() != "rise must be > 0" {
		t.Log(err.Error())
		t.Fail()
	}
}

func TestHealthcheckValidateFailFall(t *testing.T) {
	h := Healthcheck{
		Type: "ping",
		Rise: 1,
	}
	err := h.Validate()
	if err == nil {
		t.Fail()
	}
	if err.Error() != "fall must be > 0" {
		t.Log(err.Error())
		t.Fail()
	}
}

func TestConfigDefault(t *testing.T) {
	c := Config{} // FIXME
	c.Default()
	// FIXME check things
}

func TestConfigValidate(t *testing.T) {
	c := Config{} // FIXME needs to be sane
	c.Default()
	err := c.Validate("foo")
	if err != nil {
		t.Log(err)
		t.Fail()
	}
}

// FIXME - need tests for each part of config failing, and check errors.

func TestUpsertRoutesSpecDefault(t *testing.T) {
	u := UpsertRoutesSpec{
		Cidr: "127.0.0.1",
	}
	u.Default()
	if u.Cidr != "127.0.0.1/32" {
		t.Log("Not canonicalized in UpsertRoutesSpecDefault")
		t.Fail()
	}
	if u.Instance != "SELF" {
		t.Log("Instance not defaulted to SELF")
	}
}

func TestUpsertRoutesSpecValidateBadInstance(t *testing.T) {
	r := UpsertRoutesSpec{
		Instance: "vpc-1234",
		Cidr:     "127.0.0.1",
	}
	err := r.Validate("foo")
	if err == nil {
		t.Fail()
	}
	// FIXME Check error
}

func TestUpsertRoutesSpecValidateMissingCidr(t *testing.T) {
	r := UpsertRoutesSpec{
		Instance: "SELF",
	}
	err := r.Validate("foo")
	if err == nil {
		t.Fail()
	}
	// FIXME Check error
}

func TestUpsertRoutesSpecValidateBadCidr1(t *testing.T) {
	r := UpsertRoutesSpec{
		Cidr:     "300.0.0.0/16",
		Instance: "SELF",
	}
	err := r.Validate("foo")
	if err == nil {
		t.Fail()
	}
	// FIXME Check error
}

func TestUpsertRoutesSpecValidateBadCidr2(t *testing.T) {
	r := UpsertRoutesSpec{
		Cidr:     "3.0.0.0/160",
		Instance: "SELF",
	}
	err := r.Validate("foo")
	if err == nil {
		t.Fail()
	}
	// FIXME Check error
}

func TestUpsertRoutesSpecValidateBadCidr3(t *testing.T) {
	r := UpsertRoutesSpec{
		Cidr:     "foo",
		Instance: "SELF",
	}
	err := r.Validate("foo")
	if err == nil {
		t.Fail()
	}
	// FIXME Check error
}

func TestUpsertRoutesSpecValidate(t *testing.T) {
	r := UpsertRoutesSpec{
		Cidr:     "0.0.0.0/0",
		Instance: "SELF",
	}
	err := r.Validate("foo")
	if err != nil {
		t.Log(err)
		t.Fail()
	}
}

func TestRouteFindSpecDefault(t *testing.T) {
	r := RouteFindSpec{}
	r.Default()
	if r.Config == nil {
		t.Fail()
	}
}
func TestRouteFindSpecValidate(t *testing.T) {
	c := make(map[string]string)
	c["key"] = "Name"
	c["value"] = "private a"
	r := RouteFindSpec{
		Type:   "by_tag",
		Config: c,
	}
	err := r.Validate("foo")
	if err != nil {
		t.Log(err)
		t.Fail()
	}
}

func TestRouteFindSpecValidateNoType(t *testing.T) {
	c := make(map[string]string)
	c["key"] = "Name"
	c["value"] = "private a"
	r := RouteFindSpec{
		Config: c,
	}
	err := r.Validate("foo")
	if err == nil {
		t.Fail()
	}
	// FIXME Check error
}

func TestRouteFindSpecValidateNoConfig(t *testing.T) {
	r := RouteFindSpec{
		Type: "by_tag",
	}
	err := r.Validate("foo")
	if err == nil {
		t.Fail()
	}
	// FIXME Check error
}

func TestRouteTableDefault(t *testing.T) {
	r := RouteTable{}
	r.Default()
}

func TestRouteTableValidateNullRoutes(t *testing.T) {
	r := RouteTable{}
	err := r.Validate("foo")
	if err == nil {
		t.Fail()
	}
	// FIXME Check error
}

func TestRouteTableValidateNoRoutes(t *testing.T) {
	r := RouteTable{
		UpsertRoutes: make([]UpsertRoutesSpec, 0),
	}
	err := r.Validate("foo")
	if err == nil {
		t.Fail()
	}
	// FIXME Check error
}

func TestRouteTableValidate(t *testing.T) {
	routes := make([]UpsertRoutesSpec, 1)
	routes[0] = UpsertRoutesSpec{}
	r := RouteTable{
		UpsertRoutes: routes,
	}
	r.Default()
	err := r.Validate("foo")
	if err != nil {
		t.Fail()
	}
}
