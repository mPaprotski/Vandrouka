-- +goose Up
CREATE TABLE user_achievements (
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 code TEXT NOT NULL CHECK(code IN ('first_discovery','five_discoveries','ten_discoveries')),
 earned_at TIMESTAMPTZ NOT NULL,
 PRIMARY KEY(user_id,code)
);
-- +goose StatementBegin
CREATE FUNCTION award_discovery_achievements() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 total BIGINT;
BEGIN
 PERFORM 1 FROM users WHERE id=NEW.user_id FOR UPDATE;
 SELECT count(*) INTO total FROM xp_transactions WHERE user_id=NEW.user_id;
 INSERT INTO user_achievements(user_id,code,earned_at)
 SELECT NEW.user_id,d.code,NEW.created_at
 FROM (VALUES ('first_discovery',1),('five_discoveries',5),('ten_discoveries',10)) AS d(code,threshold)
 WHERE total>=d.threshold
 ON CONFLICT(user_id,code) DO NOTHING;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER award_discoveries AFTER INSERT ON xp_transactions
 FOR EACH ROW EXECUTE FUNCTION award_discovery_achievements();
-- Backfill previous confirmed discoveries deterministically, including hidden history.
INSERT INTO user_achievements(user_id,code,earned_at)
SELECT x.user_id,d.code,x.created_at FROM (
 SELECT user_id,created_at,row_number() OVER(PARTITION BY user_id ORDER BY created_at,id) AS n FROM xp_transactions
) x JOIN (VALUES ('first_discovery',1),('five_discoveries',5),('ten_discoveries',10)) d(code,threshold)
 ON x.n=d.threshold;
-- +goose Down
DROP TRIGGER award_discoveries ON xp_transactions;
DROP FUNCTION award_discovery_achievements();
DROP TABLE user_achievements;
