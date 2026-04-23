ALTER TABLE hotel_images DROP COLUMN IF EXISTS bucket;
ALTER TABLE hotel_images DROP COLUMN IF EXISTS content_type;

ALTER TABLE room_images DROP COLUMN IF EXISTS bucket;
ALTER TABLE room_images DROP COLUMN IF EXISTS content_type;
