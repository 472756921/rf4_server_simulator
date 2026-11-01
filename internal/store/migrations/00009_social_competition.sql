-- 成就、社交查询与竞赛状态机。
CREATE TABLE IF NOT EXISTS achievement_def(
    id   INTEGER PRIMARY KEY,
    sort INTEGER NOT NULL
);
INSERT OR IGNORE INTO achievement_def(id, sort) VALUES
(101,0),(102,1),(103,2),(104,3),(105,4),(107,5),(109,6),(110,7),(108,8),(106,9),
(151,10),(152,11),(153,12),(154,13),(155,14),(156,15),(157,16),(158,17),(159,18),(160,19),(161,20),(162,21),
(200,22),(202,23),(203,24),(204,25),(205,26),(207,27),(208,28),(212,29),(213,30),(214,31),(215,32),(217,33),(216,34),(218,35),(209,36),(210,37),(206,38),(211,39),
(250,40),(251,41),(252,42),(253,43),(254,44),(255,45),(256,46),(257,47),(258,48),(259,49),(261,50),(262,51),(264,52),(265,53),(266,54),(267,55),(269,56),(270,57),(271,58),(272,59),(273,60),(274,61),(275,62),(260,63),
(300,64),(301,65),(302,66),(303,67),(304,68),(305,69),(306,70),(307,71),(308,72),(309,73),(310,74),(311,75),(320,76),
(350,77),(351,78),(352,79),(353,80),(354,81),(355,82),(356,83),(357,84),(358,85),(359,86),(360,87),(361,88),(362,89),(363,90),(364,91),(365,92),
(900,93),(901,94),(902,95),(903,96),(904,97),(905,98),(906,99),(907,100),(908,101),(909,102),(240,103),(911,104),(912,105),
(1000,106),(1001,107),(1002,108),(1003,109),(1004,110),
(400,111),(401,112),(402,113),(403,114),(404,115),(405,116),(406,117),(417,118),(410,119),(411,120),(412,121),(413,122),(414,123),(415,124),(416,125),(409,126),(420,127),(421,128),(422,129);

ALTER TABLE competition ADD COLUMN duration_sec INTEGER NOT NULL DEFAULT 3600;
ALTER TABLE competition ADD COLUMN started_ts REAL NOT NULL DEFAULT 0;
ALTER TABLE competition ADD COLUMN finished_ts REAL NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS competition_result(
    comp_id    TEXT NOT NULL,
    uid        INTEGER NOT NULL,
    rank       INTEGER NOT NULL,
    score      INTEGER NOT NULL DEFAULT 0,
    weight_g   INTEGER NOT NULL DEFAULT 0,
    grade      INTEGER NOT NULL DEFAULT 1,
    updated_ts REAL NOT NULL DEFAULT 0,
    PRIMARY KEY(comp_id, uid)
);

CREATE INDEX IF NOT EXISTS idx_catch_log_uid_ts ON catch_log(uid, ts);
CREATE INDEX IF NOT EXISTS idx_player_achievements_uid ON player_achievements(uid, ach_id);
CREATE INDEX IF NOT EXISTS idx_competition_state_created ON competition(state, created_ts);
CREATE INDEX IF NOT EXISTS idx_competition_member_uid ON competition_member(uid, comp_id);
CREATE INDEX IF NOT EXISTS idx_competition_result_comp_rank ON competition_result(comp_id, rank, uid);
