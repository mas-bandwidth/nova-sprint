package store

import (
 "testing"
 "github.com/mas-bandwidth/nova-sprint/internal/sprint"
 "github.com/mas-bandwidth/nova-sprint/pkg/cardcost"
 "github.com/stretchr/testify/require"
)

func TestSubscriptionAppliedBillingInFakeStore(t *testing.T) {
 t.Parallel()
 h:=newHarness(t)
 h.m.SetProviders(map[string]sprint.Provider{"p":{Name:"p",Billing:"subscription",Funded:true}})
 h.m.SetRoutes([]sprint.Route{{Name:"inherited",Provider:"p"},{Name:"explicit",Provider:"p",Prices:cardcost.Prices{Billing:"plan",Input:"0.3"}}})
 routes,err:=h.st.priceRoutes(h.ctx);require.NoError(t,err)
 require.Equal(t,"plan",routes[0].Prices.Billing,"explicit route declaration wins")
 require.Equal(t,"subscription",routes[1].Prices.Billing,"only missing route declaration inherits")
 h.m.SetRoutes([]sprint.Route{{Name:"bad",Prices:cardcost.Prices{Billing:"unknown"}}})
 _,err=h.st.priceRoutes(h.ctx);require.Error(t,err);require.Contains(t,err.Error(),"adopt the paired")
 _,err=h.st.routes(h.ctx);require.Error(t,err)
}
