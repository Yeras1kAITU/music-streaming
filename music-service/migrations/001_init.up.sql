CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS tracks (
                                      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                      user_id UUID NOT NULL,
                                      title VARCHAR(255) NOT NULL,
                                      artist VARCHAR(255) NOT NULL,
                                      album VARCHAR(255),
                                      duration INT,
                                      genre VARCHAR(50),
                                      url TEXT NOT NULL,
                                      plays BIGINT DEFAULT 0,
                                      likes BIGINT DEFAULT 0,
                                      created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                                      updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS playlists (
                                         id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                         user_id UUID NOT NULL,
                                         name VARCHAR(255) NOT NULL,
                                         description TEXT,
                                         is_public BOOLEAN DEFAULT FALSE,
                                         created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                                         updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS playlist_tracks (
                                               playlist_id UUID REFERENCES playlists(id) ON DELETE CASCADE,
                                               track_id UUID REFERENCES tracks(id) ON DELETE CASCADE,
                                               added_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                                               PRIMARY KEY (playlist_id, track_id)
);

CREATE TABLE IF NOT EXISTS likes (
                                     user_id UUID NOT NULL,
                                     track_id UUID NOT NULL,
                                     created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                                     PRIMARY KEY (user_id, track_id)
);

CREATE INDEX idx_tracks_user_id ON tracks(user_id);
CREATE INDEX idx_tracks_artist ON tracks(artist);
CREATE INDEX idx_tracks_title ON tracks(title);
CREATE INDEX idx_playlists_user_id ON playlists(user_id);