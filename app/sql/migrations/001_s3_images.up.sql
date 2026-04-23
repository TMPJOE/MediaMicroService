-- Table to store S3 object URLs for hotel images.
-- Object keys follow the pattern: hotels/{hotelID}/{timestamp}-{filename}
CREATE TABLE IF NOT EXISTS hotel_images (
    id          BIGSERIAL PRIMARY KEY,
    hotel_id    UUID        NOT NULL,
    object_key  TEXT        NOT NULL,
    bucket      TEXT        NOT NULL DEFAULT 'media',
    content_type TEXT       NOT NULL DEFAULT 'image/jpeg',
    file_size   BIGINT      NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_hotel_images_object_key UNIQUE (object_key)
);

CREATE INDEX idx_hotel_images_hotel_id ON hotel_images (hotel_id);

-- Table to store S3 object URLs for room images.
-- Object keys follow the pattern: rooms/{roomID}/{timestamp}-{filename}
CREATE TABLE IF NOT EXISTS room_images (
    id          BIGSERIAL PRIMARY KEY,
    room_id     UUID        NOT NULL,
    object_key  TEXT        NOT NULL,
    bucket      TEXT        NOT NULL DEFAULT 'media',
    content_type TEXT       NOT NULL DEFAULT 'image/jpeg',
    file_size   BIGINT      NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_room_images_object_key UNIQUE (object_key)
);

CREATE INDEX idx_room_images_room_id ON room_images (room_id);
