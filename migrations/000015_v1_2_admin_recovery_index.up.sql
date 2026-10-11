-- V12-02d follow-up: the schema contract requires every foreign key to carry
-- a supporting leading index. admin_recovery_tickets.account_id was missing
-- one at 000011; this forward migration adds it without touching the
-- immutable history.
CREATE INDEX idx_admin_recovery_tickets_account ON public.admin_recovery_tickets (account_id);
