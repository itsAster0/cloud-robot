package enginev4

import "testing"

func TestV4ConfigurationLimits(t *testing.T) {
	c := Config{}
	c.Defaults()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.Capacity = 257
	if c.Validate() == nil {
		t.Fatal("257 accepted")
	}
	c.Capacity = 255
	c.Mode = "br-squad"
	if c.Validate() == nil {
		t.Fatal("partial squad capacity accepted")
	}
	c.Capacity = 256
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.SiteCount = 16
	c.CoverPerSite = 8
	c.LootPerSite = 24
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.SiteCount = 257
	if c.Validate() == nil {
		t.Fatal("257 sites accepted")
	}
	c.SiteCount = 16
	c.CoverPerSite = 13
	if c.Validate() == nil {
		t.Fatal("13 cover accepted")
	}
	c.CoverPerSite = 8
	c.LootPerSite = 33
	if c.Validate() == nil {
		t.Fatal("33 loot accepted")
	}
}
func TestV4BuildBudget(t *testing.T) {
	l := Loadout{Chassis: "heavy", Weapon: "railgun", Modules: []string{"optics", "capacitor"}}
	if l.Validate() == nil {
		t.Fatal("over-budget build accepted")
	}
	l.Chassis = "generalist"
	l.Modules = []string{"optics", "optics"}
	if l.Validate() == nil {
		t.Fatal("duplicate module accepted")
	}
	l.Modules = []string{"optics", "cooling_system"}
	if err := l.Validate(); err != nil {
		t.Fatal(err)
	}
}
