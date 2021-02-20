prefix_path = "/api/v2"

annotations = {
    "company/version": 1
}

upstream "accounts" {
    destination = "${destination}" 
    owner = "Identity <team-identity@company.com>"
    prefix_path = "/private"

    route {
        methods = ["GET"]
        path = "/accounts/{id}"
    }

    route {
        methods = ["GET"]
        path = "/accounts"
    }
}
