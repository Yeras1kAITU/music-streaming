CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS subscriptions (
                                             id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                             user_id UUID NOT NULL,
                                             plan_id VARCHAR(100) NOT NULL,
                                             plan_name VARCHAR(100) NOT NULL,
                                             status VARCHAR(50) DEFAULT 'active',
                                             price DECIMAL(10,2) NOT NULL,
                                             currency VARCHAR(3) DEFAULT 'USD',
                                             start_date TIMESTAMP NOT NULL,
                                             end_date TIMESTAMP NOT NULL,
                                             created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                                             updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS payment_transactions (
                                                    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                                    user_id UUID NOT NULL,
                                                    subscription_id UUID,
                                                    amount DECIMAL(10,2) NOT NULL,
                                                    currency VARCHAR(3) DEFAULT 'USD',
                                                    status VARCHAR(50) DEFAULT 'pending',
                                                    payment_method VARCHAR(50) NOT NULL,
                                                    description TEXT,
                                                    receipt_url TEXT,
                                                    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
                                                    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS coupons (
                                       id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                       code VARCHAR(50) UNIQUE NOT NULL,
                                       discount DECIMAL(5,2) NOT NULL,
                                       used_by UUID,
                                       used_at TIMESTAMP,
                                       expires_at TIMESTAMP NOT NULL,
                                       created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pricing_plans (
                                             id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                                             name VARCHAR(100) NOT NULL,
                                             price DECIMAL(10,2) NOT NULL,
                                             currency VARCHAR(3) DEFAULT 'USD',
                                             interval VARCHAR(20) DEFAULT 'month',
                                             quality INT DEFAULT 128,
                                             offline_mode BOOLEAN DEFAULT FALSE,
                                             created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_subscriptions_user_id ON subscriptions(user_id);
CREATE INDEX idx_subscriptions_status ON subscriptions(status);
CREATE INDEX idx_payments_user_id ON payment_transactions(user_id);
CREATE INDEX idx_coupons_code ON coupons(code);