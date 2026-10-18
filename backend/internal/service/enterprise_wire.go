package service

import (
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
)

// ProvideAdminService installs the enterprise guard without widening AdminService.
func ProvideAdminService(
	cfg *config.Config,
	userRepo UserRepository,
	groupRepo AdminGroupRepository,
	accountRepo AdminAccountRepository,
	proxyRepo ProxyRepository,
	apiKeyRepo APIKeyRepository,
	redeemCodeRepo RedeemCodeRepository,
	userGroupRateRepo UserGroupRateRepository,
	userRPMCache UserRPMCache,
	billingCacheService *BillingCacheService,
	proxyProber ProxyExitInfoProber,
	proxyLatencyCache ProxyLatencyCache,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	entClient *dbent.Client,
	settingService *SettingService,
	defaultSubAssigner DefaultSubscriptionAssigner,
	userSubRepo UserSubscriptionRepository,
	privacyClientFactory PrivacyClientFactory,
	runtimeBlocker AccountRuntimeBlocker,
	affiliateService *AffiliateService,
	compositeRouteRepo CompositeModelRouteRepository,
	compositeResolver *CompositeRouteResolver,
	channelCacheInvalidator ChannelCacheInvalidator,
	enterprises EnterpriseMemberRepository,
) AdminService {
	svc := NewAdminService(cfg, userRepo, groupRepo, accountRepo, proxyRepo, apiKeyRepo, redeemCodeRepo, userGroupRateRepo, userRPMCache, billingCacheService, proxyProber, proxyLatencyCache, authCacheInvalidator, entClient, settingService, defaultSubAssigner, userSubRepo, privacyClientFactory, runtimeBlocker, affiliateService, compositeRouteRepo, compositeResolver, channelCacheInvalidator)
	if guard, ok := svc.(interface {
		SetEnterpriseUserGuard(EnterpriseMemberRepository)
	}); ok {
		guard.SetEnterpriseUserGuard(enterprises)
	}
	return svc
}

func ProvideAffiliateService(repo AffiliateRepository, settings *SettingService, auth APIKeyAuthCacheInvalidator, billing *BillingCacheService, enterprises EnterpriseRepository) *AffiliateService {
	svc := NewAffiliateService(repo, settings, auth, billing)
	svc.SetEnterpriseMembershipReader(enterprises)
	return svc
}

// ProvideEnterpriseService connects password resets to the existing refresh-session revoker.
func ProvideEnterpriseService(client *dbent.Client, enterprises EnterpriseRepository, departments EnterpriseDepartmentRepository, members EnterpriseMemberRepository, quotas EnterpriseQuotaRepository, balanceLogs EnterpriseBalanceLogRepository, auditLogs EnterpriseAuditLogRepository, enterpriseGroups EnterpriseGroupRepository, users UserRepository, groups GroupRepository, admin AdminService, authCache APIKeyAuthCacheInvalidator, billingCache EnterpriseBillingCache, auth *AuthService) *EnterpriseService {
	s := NewEnterpriseService(client, enterprises, departments, members, quotas, balanceLogs, auditLogs, enterpriseGroups, users, groups, admin, authCache, billingCache)
	s.sessions = auth
	return s
}
