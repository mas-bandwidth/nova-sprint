------------------------ MODULE MCJudgmentRecheck ------------------------
EXTENDS JudgmentRecheck
CONSTANTS Late, Route, Orphan, Other
ModelKinds == [j \in Judgments |->
  CASE j = Late -> "late" [] j = Route -> "route" [] j = Orphan -> "orphan"
       [] OTHER -> "other"]
=============================================================================
