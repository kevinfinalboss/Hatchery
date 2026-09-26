-- Pruning deletes by age: without this index every prune scans the whole table.
CREATE INDEX audit_events_created_idx ON audit_events (created_at);
