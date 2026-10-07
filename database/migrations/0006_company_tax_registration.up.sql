-- ZIMRA tax registration (BP number / tax clearance reference). Recorded
-- for display and future fiscalisation integration only — System
-- Documentation 5.0 section 3.3/8.3 is explicit that Zimbabwe tax and
-- fiscalisation obligations need separate verification, so this column is
-- informational and never validated against ZIMRA by the application.
ALTER TABLE companies
    ADD COLUMN zimra_tax_number varchar(20);
