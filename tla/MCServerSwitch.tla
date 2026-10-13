---------------------------- MODULE MCServerSwitch ----------------------------
\* A bounded instance of ServerSwitch: the constants the configs pin
\* (MCServerSwitch.cfg, MCServerSwitchLive.cfg, MCServerSwitchBrokenNodeadline.cfg,
\* MCServerSwitchBrokenKeepstate.cfg, MCServerSwitchBrokenHookfault.cfg,
\* MCServerSwitchBrokenHookGate.cfg, MCServerSwitchBrokenSilentdedupe.cfg,
\* MCServerSwitchBrokenLatejudge.cfg, MCServerSwitchBrokenPartialrefusal.cfg,
\* MCServerSwitchBrokenRawkey.cfg, MCServerSwitchBrokenRejudge.cfg,
\* MCServerSwitchBrokenIdleclears.cfg, MCServerSwitchBrokenAckcloses.cfg,
\* MCServerSwitchBrokenStaleack.cfg).
\* The module holds no rule of its own;
\* ServerSwitch is the model (internal/sprint/server_switch.go).
\*-----------------------------------------------------------------------------
EXTENDS ServerSwitch

MCBinaries == {"b0", "b1", "b2"}
MCCandidate == [n \in 1..2 |-> IF n = 1 THEN "b1" ELSE "b2"]
=============================================================================
