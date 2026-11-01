package store

import (
	"context"
	"errors"
	"testing"
)

func TestCompetitionLifecycleAndPermissions(t *testing.T) {
	db := openSocialTestDB(t)
	ctx := context.Background()
	comp := &Competition{
		ID: "comp-1", Name: "Cup", State: CompetitionCreated, Owner: 1,
		MaxMembers: 4, CondJSON: `{"win_cond":2}`, DurationSec: 3600,
	}
	if err := db.CreateCompetition(ctx, comp, true); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetCompetition(ctx, comp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != CompetitionRegistration || len(got.Members) != 1 {
		t.Fatalf("created=%+v", got)
	}
	if _, joined, err := db.JoinCompetition(ctx, comp.ID, 2, 0); err != nil || !joined {
		t.Fatalf("join: joined=%v err=%v", joined, err)
	}
	if _, joined, err := db.JoinCompetition(ctx, comp.ID, 2, 0); err != nil || joined {
		t.Fatalf("repeat join: joined=%v err=%v", joined, err)
	}
	if _, changed, err := db.SetCompetitionReady(ctx, comp.ID, 2, true); err != nil || !changed {
		t.Fatalf("ready: changed=%v err=%v", changed, err)
	}
	if _, changed, err := db.SetCompetitionReady(ctx, comp.ID, 2, true); err != nil || changed {
		t.Fatalf("repeat ready: changed=%v err=%v", changed, err)
	}
	renamed := "Renamed"
	if _, err := db.UpdateCompetition(ctx, comp.ID, 2, CompetitionPatch{Name: &renamed}); !errors.Is(err, ErrCompetitionForbidden) {
		t.Fatalf("non-owner update err=%v", err)
	}
	if updated, err := db.UpdateCompetition(ctx, comp.ID, 1, CompetitionPatch{Name: &renamed}); err != nil || updated.Name != renamed {
		t.Fatalf("owner update=%+v err=%v", updated, err)
	}
	if _, err := db.StartCompetition(ctx, comp.ID, 2); !errors.Is(err, ErrCompetitionForbidden) {
		t.Fatalf("non-owner start err=%v", err)
	}
	if _, err := db.KickCompetitionMember(ctx, comp.ID, 2, 1); !errors.Is(err, ErrCompetitionForbidden) {
		t.Fatalf("non-owner kick err=%v", err)
	}
	kicked, err := db.KickCompetitionMember(ctx, comp.ID, 1, 2)
	if err != nil || len(kicked.Members) != 1 {
		t.Fatalf("kick=%+v err=%v", kicked, err)
	}
	if _, joined, err := db.JoinCompetition(ctx, comp.ID, 2, 0); err != nil || !joined {
		t.Fatalf("rejoin: joined=%v err=%v", joined, err)
	}
	started, err := db.StartCompetition(ctx, comp.ID, 1)
	if err != nil || started.State != CompetitionRunning || started.StartedTS <= 0 {
		t.Fatalf("started=%+v err=%v", started, err)
	}
}

func TestCompetitionLeaveTransfersOwnerAndResultsPersist(t *testing.T) {
	db := openSocialTestDB(t)
	ctx := context.Background()
	comp := &Competition{
		ID: "comp-2", Name: "Cup", State: CompetitionCreated, Owner: 10,
		MaxMembers: 4, CondJSON: `{"win_cond":8}`, DurationSec: 3600,
	}
	if err := db.CreateCompetition(ctx, comp, true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.JoinCompetition(ctx, comp.ID, 20, 0); err != nil {
		t.Fatal(err)
	}
	afterLeave, err := db.LeaveCompetition(ctx, comp.ID, 10)
	if err != nil || afterLeave.Owner != 20 || len(afterLeave.Members) != 1 {
		t.Fatalf("after leave=%+v err=%v", afterLeave, err)
	}
	if _, err := db.StartCompetition(ctx, comp.ID, 20); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishCompetition(ctx, comp.ID); err != nil {
		t.Fatal(err)
	}
	rows := []CompetitionResult{
		{CompID: comp.ID, UID: 20, Rank: 1, Score: 900, WeightG: 900, Grade: 2, UpdatedTS: 100},
	}
	if err := db.SaveCompetitionResults(ctx, comp.ID, rows); err != nil {
		t.Fatal(err)
	}
	saved, err := db.ListCompetitionResults(ctx, comp.ID)
	if err != nil || len(saved) != 1 || saved[0].UID != 20 || saved[0].Rank != 1 {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
}
