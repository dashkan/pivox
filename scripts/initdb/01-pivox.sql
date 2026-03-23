-- Create pivox role with database creation privileges
CREATE ROLE pivox WITH LOGIN CREATEDB;

-- Create pivox database owned by pivox role
CREATE DATABASE pivox OWNER pivox;
