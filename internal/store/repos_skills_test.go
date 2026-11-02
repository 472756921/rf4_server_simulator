package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestSkillProficiencyCap(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "skills.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	const uid = int64(1000001)

	if err := db.AddSkillPoints(ctx, uid, 1, 99.9); err != nil {
		t.Fatal(err)
	}
	if err := db.AddSkillPoints(ctx, uid, 1, 0.2); err != nil {
		t.Fatal(err)
	}
	if pts, _ := db.GetSkillPoints(ctx, uid, 1); pts != maxSkillPoints {
		t.Fatalf("熟练度应封顶 %v，实得 %v", maxSkillPoints, pts)
	}
	all, _ := db.ListSkills(ctx, uid)
	if all[1] != maxSkillPoints {
		t.Fatalf("列表熟练度应封顶 %v，实得 %v", maxSkillPoints, all[1])
	}

	if _, err := db.SQL.Exec(`UPDATE skill SET points=110 WHERE uid=? AND skill_id=1`, uid); err != nil {
		t.Fatal(err)
	}
	if pts, _ := db.GetSkillPoints(ctx, uid, 1); pts != maxSkillPoints {
		t.Fatalf("历史熟练度应在读取时封顶，实得 %v", pts)
	}
	if _, err := db.SQL.Exec(`DELETE FROM schema_migrations WHERE name='migrations/00017_skill_points_cap.sql'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	var stored float64
	if err := db.SQL.QueryRow(`SELECT points FROM skill WHERE uid=? AND skill_id=1`, uid).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != maxSkillPoints {
		t.Fatalf("迁移后历史熟练度应封顶 %v，实得 %v", maxSkillPoints, stored)
	}
}
