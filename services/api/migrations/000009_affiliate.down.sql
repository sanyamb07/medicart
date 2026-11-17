DROP TABLE IF EXISTS affiliate_referrals;
DROP TABLE IF EXISTS affiliates;
DROP TABLE IF EXISTS affiliate_settings;
DELETE FROM roles WHERE code = 'AFFILIATE';
