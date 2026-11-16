-- The format of a message: html, txt or markdown. It decides how the text is
-- rendered and which PushPlus template is used when the message is sent.
ALTER TABLE strategies ADD COLUMN format TEXT NOT NULL DEFAULT 'txt';

-- The format is kept with the queued message, so that a later edit of the
-- strategy does not change a message that is already waiting in the queue.
ALTER TABLE notification_outbox ADD COLUMN format TEXT NOT NULL DEFAULT '';

-- The project has a person in charge, so that a reminder can name one even
-- when the device itself has no owner.
ALTER TABLE projects ADD COLUMN owner TEXT NOT NULL DEFAULT '';
