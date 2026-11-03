package gameserver

func completedCatchPending(st *FishState) bool {
	return st.Pending != nil && st.Hooked && !st.CastActive && !st.Resolved
}

func (s *Server) pendingCatchAllowsCast(ctx *Ctx, st *FishState) bool {
	st.mu.Lock()
	blocked := completedCatchPending(st)
	st.mu.Unlock()
	if blocked {
		ctx.ReplyFailure("System.InvalidOperationException", "请先处理本次钓获")
	}
	return !blocked
}
