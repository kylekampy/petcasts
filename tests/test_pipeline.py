from petcast.pipeline import _is_moderation_error


def test_is_moderation_error_matches_common_policy_messages():
    assert _is_moderation_error(Exception("moderation_blocked"))
    assert _is_moderation_error(Exception("content_policy violation"))
    assert _is_moderation_error(Exception("request was rejected by the safety system"))


def test_is_moderation_error_does_not_match_auth_errors():
    assert not _is_moderation_error(Exception("401 unauthorized: invalid API key"))
