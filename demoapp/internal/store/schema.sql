CREATE TABLE IF NOT EXISTS products (
  id          INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  sku         VARCHAR(32)  NOT NULL,
  name        VARCHAR(200) NOT NULL,
  description TEXT         NOT NULL,
  price_cents INT UNSIGNED NOT NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_products_sku (sku)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS customers (
  id         INT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  email      VARCHAR(200) NOT NULL,
  name       VARCHAR(200) NOT NULL,
  created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_customers_email (email)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS orders (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  customer_id INT UNSIGNED    NOT NULL,
  product_id  INT UNSIGNED    NOT NULL,
  quantity    INT UNSIGNED    NOT NULL,
  total_cents INT UNSIGNED    NOT NULL,
  status      VARCHAR(16)     NOT NULL DEFAULT 'pending',
  payment_ref VARCHAR(64)     NOT NULL DEFAULT '',
  created_at  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  KEY idx_orders_customer_created (customer_id, created_at),
  KEY idx_orders_status_created (status, created_at)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS order_queue (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  order_id    BIGINT UNSIGNED NOT NULL,
  enqueued_at DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS daily_sales (
  day           DATE            NOT NULL PRIMARY KEY,
  orders        INT UNSIGNED    NOT NULL,
  revenue_cents BIGINT UNSIGNED NOT NULL,
  computed_at   DATETIME(3)     NOT NULL
) ENGINE=InnoDB;
