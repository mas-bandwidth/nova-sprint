package store

import (
 "context"
 "fmt"
 "slices"
 "strings"

 "github.com/mas-bandwidth/nova-sprint/internal/sprint"
)

// PlanningHistory projects the existing epoch logs for an explicit plan review
// (docs/SPEC-SPRINT.md, plan and check columns). Clear retains those logs. No new
// catalog key or version is written, and no tick pays for this on-demand read.
func (st *Store) PlanningHistory(ctx context.Context) ([]sprint.PlanningRecord,error) {
 pinned,err:=st.pin(ctx); if err!=nil { return nil,err }
 var out []sprint.PlanningRecord
 for epoch:=uint64(0); epoch<=pinned.epoch; epoch++ {
  old:=pinned.At(epoch)
  cards:=map[string]*sprint.PlanningRecord{}
  primary:=map[string]string{}
  reported:=map[string]bool{}
  record:=func(id string) *sprint.PlanningRecord { if cards[id]==nil { cards[id]=&sprint.PlanningRecord{ID:id,Epoch:epoch} }; return cards[id] }
  after:=""
  for {
   lines,ids,err:=old.B.LogSince(ctx,after,logPage); if err!=nil { return nil,fmt.Errorf("planning catalog epoch %d: %w",epoch,err) }
   for _,line:=range lines {
    if line.Kind!=sprint.LineMove { continue }
    for _,id:=range line.Names() {
     switch line.Table {
     case sprint.Work:
      c:=record(id)
      if v,ok:=line.Text["brief"]; ok { c.Brief=v }
      if v,ok:=line.Set[sprint.FieldRepo]; ok { c.Repo=v }
      if strings.HasSuffix(line.To,":"+sprint.Landed) { c.Landed=true }
     case sprint.Fleet:
      if line.Primary!="" { primary[id]=line.Primary }; if v:=line.Set["primary"];v!="" { primary[id]=v }
      if line.Set["ok"]!="no" || primary[id]=="" || reported[id] { continue }
      text:=line.Text["report"]; if text=="" { text=line.Text["reason"] }
      first,_,_:=strings.Cut(strings.TrimSpace(text),"\n")
      if first!="" { record(primary[id]).Failures=append(record(primary[id]).Failures,id+": "+first); reported[id]=true }
     }
    }
   }
   if len(ids)<logPage { break }; after=ids[len(ids)-1]
  }
  var keys []string; for id:=range cards { keys=append(keys,id) }; slices.Sort(keys)
  for _,id:=range keys { c:=cards[id]; if c.Brief!="" && (c.Landed || len(c.Failures)>0) { out=append(out,*c) } }
 }
 return out,nil
}
