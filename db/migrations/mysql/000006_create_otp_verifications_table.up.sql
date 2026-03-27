-- otp_verifications: Pending OTP records for email-based authentication.

CREATE TABLE IF NOT EXISTS otp_verifications (
    email      VARCHAR(255) NOT NULL,
    app_id     VARCHAR(64)  NOT NULL,
    otp_hash   VARCHAR(255) NOT NULL,
    expires_at TIMESTAMP    NOT NULL,
    created_at TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,

    PRIMARY KEY (app_id, email)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
