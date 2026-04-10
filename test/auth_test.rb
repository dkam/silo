require_relative "test_helper"
require "securerandom"

class AuthTest < Minitest::Test
  include SeafileTestHelper

  def test_login_returns_jwt
    c = SeafileClient.new(seafile_url)
    resp = c.login(seafile_email, seafile_password)
    assert resp.ok?, "Login failed: #{resp}"
    assert resp["token"], "Expected token in response"
    assert resp["token"].include?("."), "Token should be a JWT (contains dots)"
  end

  def test_login_wrong_password
    c = SeafileClient.new(seafile_url)
    resp = c.post("/api/v1/auth/login", { email: seafile_email, password: "wrong" }, auth: false)
    assert_equal 401, resp.status
  end

  def test_login_missing_fields
    c = SeafileClient.new(seafile_url)

    resp = c.post("/api/v1/auth/login", { email: seafile_email }, auth: false)
    assert_equal 400, resp.status

    resp = c.post("/api/v1/auth/login", { password: "whatever" }, auth: false)
    assert_equal 400, resp.status
  end

  def test_login_nonexistent_user
    c = SeafileClient.new(seafile_url)
    resp = c.post("/api/v1/auth/login", { email: "nobody@example.com", password: "x" }, auth: false)
    assert_equal 401, resp.status
  end

  def test_protected_endpoints_reject_no_auth
    c = SeafileClient.new(seafile_url)

    resp = c.request(:get, "/api/v1/repos", auth: false)
    assert_equal 401, resp.status

    resp = c.request(:post, "/api/v1/repos", body: { name: "x" }, auth: false)
    assert_equal 401, resp.status
  end

  def test_protected_endpoints_reject_bad_token
    c = SeafileClient.new(seafile_url)
    c.instance_variable_set(:@token, "not.a.valid.jwt.token")

    resp = c.list_repos
    assert_equal 401, resp.status
  end
end
