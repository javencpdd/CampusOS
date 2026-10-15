-- Owned fixture, inserted while000019 is down, never a production seed.
INSERT INTO posts(id,thread_id,author_id,author_name,content,status,floor_number,metadata)
VALUES(121101,119201,119101,'Legacy post','Original post history','published',1,'{"big":9007199254740993}');
INSERT INTO posts(id,thread_id,author_id,author_name,content,status,floor_number,metadata,deleted_at)
VALUES(121102,119201,119101,'Legacy deleted','Retained deleted body','deleted',2,'{"legacy":"no actor evidence"}',NOW());
UPDATE threads SET reply_count=1 WHERE id=119201;
