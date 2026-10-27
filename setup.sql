-- Local test data and database users for the toolbox.
-- DROPS AND RECREATES TABLES: only ever run this against a local dev database.
-- Run as root: mysql -uroot < setup.sql

CREATE DATABASE IF NOT EXISTS shop;
USE shop;

-- Read tools log in as toolbox_ro, write tools as toolbox_rw.
-- Neither can CREATE, ALTER or DROP, so schema changes are refused by MySQL itself.
CREATE USER IF NOT EXISTS 'toolbox_ro'@'localhost' IDENTIFIED BY 'dev-ro';
CREATE USER IF NOT EXISTS 'toolbox_rw'@'localhost' IDENTIFIED BY 'dev-rw';
GRANT SELECT ON shop.* TO 'toolbox_ro'@'localhost';
GRANT SELECT, INSERT, UPDATE, DELETE ON shop.* TO 'toolbox_rw'@'localhost';

DROP TABLE IF EXISTS payments, orders, customers;

CREATE TABLE customers (
  id   INT AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(100) NOT NULL
);

CREATE TABLE orders (
  id           INT AUTO_INCREMENT PRIMARY KEY,
  customer_id  INT NOT NULL,
  amount_cents INT NOT NULL,
  status       ENUM('pending', 'paid', 'cancelled') NOT NULL,
  created_at   DATETIME NOT NULL
);

CREATE TABLE payments (
  id         INT AUTO_INCREMENT PRIMARY KEY,
  order_id   INT NOT NULL,
  status     ENUM('succeeded', 'failed') NOT NULL,
  created_at DATETIME NOT NULL
);

INSERT INTO customers (name)
WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 20)
SELECT CONCAT('Customer ', i) FROM n;

-- 50 orders, all paid, each with a succeeded payment.
INSERT INTO orders (customer_id, amount_cents, status, created_at)
WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 50)
SELECT 1 + (i % 20), 1000 + i * 37, 'paid', NOW() - INTERVAL i HOUR FROM n;

INSERT INTO payments (order_id, status, created_at)
SELECT id, 'succeeded', created_at + INTERVAL 1 MINUTE FROM orders;

-- The planted bug: 3 orders were paid, but the payment webhook never updated them,
-- so they are stuck in 'pending'. The fix is a single UPDATE joining payments.
UPDATE orders SET status = 'pending' WHERE id IN (7, 19, 33);
