-- Notifications per channel (etappe 123). 020 is applied already and stays as it is.
--
-- Until now one row counted as "sent" as soon as any channel took the message, so a channel
-- that was down never got the problem, only the all-clear later. Now every channel has its
-- own record: sent_by / resolved_by map a channel name (webhook | email) to the time that
-- channel received the problem (or latest reminder) / the all-clear.
ALTER TABLE notify_state ADD COLUMN IF NOT EXISTS sent_by     JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE notify_state ADD COLUMN IF NOT EXISTS resolved_by JSONB NOT NULL DEFAULT '{}'::jsonb;

-- What was announced under the old rule reached every channel that existed then; carry it
-- over so nothing is announced twice after the update. last_sent is no longer written.
UPDATE notify_state
   SET sent_by = jsonb_build_object('webhook', last_sent, 'email', last_sent)
 WHERE last_sent IS NOT NULL AND sent_by = '{}'::jsonb;
