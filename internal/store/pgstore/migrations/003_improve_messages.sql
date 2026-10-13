ALTER TABLE messages ALTER COLUMN message TYPE VARCHAR(1000);
UPDATE messages SET reaction_count = 0 WHERE reaction_count < 0;
ALTER TABLE messages ADD CONSTRAINT messages_reaction_count_nonnegative CHECK (reaction_count >= 0);
CREATE INDEX messages_room_id_idx ON messages(room_id);

---- create above / drop below ----

DROP INDEX IF EXISTS messages_room_id_idx;
ALTER TABLE messages DROP CONSTRAINT messages_reaction_count_nonnegative;
ALTER TABLE messages ALTER COLUMN message TYPE VARCHAR(25);
