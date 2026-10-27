import importlib.util
from pathlib import Path
import unittest
import urllib.error

SPEC = importlib.util.spec_from_file_location("napcat_group", Path(__file__).with_name("check-napcat-group.py"))
SMOKE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(SMOKE)


class FakeClient:
    def __init__(self, *, logged_in=True, send=False):
        self.group, self.allow_send = "111111111", send
        self.logged_in = logged_in
        self.actions = []
        self.sent_text = None
        self.bad_readback = False
        self.wrong_group = False

    def request(self, path, body):
        return {"isLogin": self.logged_in, "isOffline": False}

    def action(self, action, params):
        self.actions.append((action, params))
        if action == "get_login_info":
            return {"user_id": 222222222}
        if action == "get_status":
            return {"online": True, "good": True}
        if action == "get_group_info":
            return {"group_id": 999999999 if self.wrong_group else int(self.group)}
        if action == "get_group_member_info":
            return {"group_id": int(self.group), "user_id": 222222222}
        if action == "send_group_msg":
            self.sent_text = params["message"]
            return {"message_id": -12345}
        if action == "get_msg":
            return {"message_id": -12345, "group_id": int(self.group), "message_type": "group", "sender": {"user_id": 222222222}, "message": "unexpected" if self.bad_readback else self.sent_text}
        raise AssertionError(action)


class NapCatGroupTests(unittest.TestCase):
    def test_offline_bot_does_not_call_group_or_send_api(self):
        client = FakeClient(logged_in=False, send=True)
        with self.assertRaises(SMOKE.LoginRequired):
            SMOKE.run_check(client)
        self.assertEqual(client.actions, [])

    def test_default_preflight_sends_nothing(self):
        client = FakeClient()
        self.assertEqual(SMOKE.run_check(client)["messages_sent"], 0)
        self.assertNotIn("send_group_msg", [x[0] for x in client.actions])

    def test_opt_in_sends_one_text_only_to_selected_group_and_reads_exact_message(self):
        client = FakeClient(send=True)
        result = SMOKE.run_check(client)
        sends = [params for action, params in client.actions if action == "send_group_msg"]
        self.assertEqual(len(sends), 1)
        self.assertEqual(sends[0]["group_id"], 111111111)
        self.assertEqual(sends[0]["message"][0]["type"], "text")
        self.assertEqual(result["messages_sent"], 1)
        self.assertTrue(result["readback_verified"])
        self.assertFalse(result["event_stream_verified"])

    def test_wrong_group_never_reaches_send(self):
        client = FakeClient(send=True)
        client.wrong_group = True
        with self.assertRaises(SMOKE.SmokeError):
            SMOKE.run_check(client)
        self.assertNotIn("send_group_msg", [x[0] for x in client.actions])

    def test_bad_readback_does_not_resend(self):
        client = FakeClient(send=True)
        client.bad_readback = True
        with self.assertRaises(SMOKE.SmokeError):
            SMOKE.run_check(client)
        self.assertEqual(sum(action == "send_group_msg" for action, _ in client.actions), 1)

    def test_transport_rejects_other_groups_and_unapproved_actions_before_network(self):
        client = SMOKE.NapCat.__new__(SMOKE.NapCat)
        client.group, client.allow_send = "111111111", True
        client.request = lambda *args: self.fail("network must not be reached")
        with self.assertRaises(SMOKE.SmokeError):
            client.action("send_group_msg", {"group_id": 999999999})
        with self.assertRaises(SMOKE.SmokeError):
            client.action("set_group_ban", {"group_id": 111111111})

    def test_send_requires_explicit_opt_in_even_for_target_group(self):
        client = SMOKE.NapCat.__new__(SMOKE.NapCat)
        client.group, client.allow_send = "111111111", False
        client.request = lambda *args: self.fail("network must not be reached")
        with self.assertRaises(SMOKE.SmokeError):
            client.action("send_group_msg", {"group_id": 111111111})

    def test_identifiers_and_non_text_readback_reject_invalid_input(self):
        for value in [True, 1.1, "0", "-1", "001", "１２３", "1e9", "12/3", None]:
            with self.subTest(value=value), self.assertRaises(SMOKE.SmokeError):
                SMOKE.identifier(value)
        with self.assertRaises(SMOKE.SmokeError):
            SMOKE.message_text([{"type": "at", "data": {"qq": "all"}}])

    def test_provider_failures_do_not_echo_credentials(self):
        class BrokenOpener:
            def open(self, *args, **kwargs):
                raise urllib.error.URLError("secret-test-credential")
        client = SMOKE.NapCat.__new__(SMOKE.NapCat)
        client.base = "http://127.0.0.1:12345"
        client.credential = "secret-test-credential"
        client.opener = BrokenOpener()
        with self.assertRaises(SMOKE.SmokeError) as caught:
            client.request("/api/QQLogin/CheckLoginStatus", {})
        self.assertNotIn(client.credential, str(caught.exception))


if __name__ == "__main__":
    unittest.main()
